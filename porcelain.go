package lightning

import (
	"log"
	"time"

	"github.com/tidwall/gjson"
)

var InvoiceListeningTimeout = time.Minute * 150
var WaitSendPayTimeout = time.Hour * 24
var WaitPaymentMaxAttempts = 60

type Client struct {
	Path             string
	PaymentHandler   func(gjson.Result)
	LastInvoiceIndex int
}

// ListenForInvoices starts a goroutine that will repeatedly call waitanyinvoice.
// Each payment received will be fed into the client.PaymentHandler function.
// You can change that function in the meantime.
// Or you can set it to nil if you want to stop listening for invoices.
func (ln *Client) ListenForInvoices() {
	go func() {
		for {
			if ln.PaymentHandler == nil {
				log.Print("won't listen for invoices: no PaymentHandler.")
				return
			}

			res, err := ln.CallWithCustomTimeout(InvoiceListeningTimeout,
				"waitanyinvoice", ln.LastInvoiceIndex)
			if err != nil {
				if _, ok := err.(ErrorTimeout); ok {
					time.Sleep(time.Minute)
				} else {
					log.Printf("error waiting for invoice %d: %s", ln.LastInvoiceIndex, err.Error())
					time.Sleep(5 * time.Second)
				}
				continue
			}

			index := res.Get("pay_index").Int()
			ln.LastInvoiceIndex = int(index)

			ln.PaymentHandler(res)
		}
	}()
}

// PayAndWaitUntilResolution implements its 'pay' logic, querying and retrying routes.
// It's like the default 'pay' plugin, but it blocks until a final success or failure is achieved.
// After it returns you can be sure a failed payment will not succeed anymore.
// Any value in params will be passed to 'getroute' or 'sendpay' or smart defaults will be used.
func (ln *Client) PayAndWaitUntilResolution(
	bolt11 string,
	params map[string]interface{},
) (success bool, payment gjson.Result, err error) {
	decoded, err := ln.Call("decodepay", bolt11)
	if err != nil {
		return false, payment, err
	}

	hash := decoded.Get("payment_hash").String()
	payee := decoded.Get("payee").String()
	msatoshi, ok := params["msatoshi"]
	if !ok {
		msatoshi = decoded.Get("msatoshi").Int()
	}
	riskfactor, ok := params["riskfactor"]
	if !ok {
		riskfactor = 10
	}
	label, ok := params["label"]
	if !ok {
		label = ""
	}

	fakePayment := gjson.Parse(`{"payment_hash": "` + hash + `"}`)

	for try := 0; try < 10; try++ {
		res, err := ln.CallNamed("getroute",
			"id", payee, "msatoshi", msatoshi, "riskfactor", riskfactor, "fuzzpercent", 0)
		if err != nil {
			return false, fakePayment, err
		}

		route := res.Get("route")
		if !route.Exists() {
			continue
		}

		// ignore returned value here as we'll get it from waitsendpay below
		_, err = ln.CallNamed("sendpay",
			"route", route.String(), "payment_hash", hash, "label", label, "bolt11", bolt11,
		)
		if err != nil {
			return false, fakePayment, err
		}

		// this should wait indefinitely, but 24h is enough
		res, err = ln.CallWithCustomTimeout(WaitSendPayTimeout, "waitsendpay", hash)
		if err != nil {
			return false, fakePayment, err
		}

		if res.Get("code").Exists() {
			switch res.Get("code").Int() {
			case 200, 202, 204:
				// try again
				continue
			default:
				return false, fakePayment, nil
			}
		}

		// payment suceeded
		return true, res, nil
	}

	return
}
