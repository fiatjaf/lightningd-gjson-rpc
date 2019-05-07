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
// This includes values from the default 'pay' plugin.
func (ln *Client) PayAndWaitUntilResolution(
	bolt11 string,
	params map[string]interface{},
) (success bool, payment gjson.Result, tries []Try, err error) {
	decoded, err := ln.Call("decodepay", bolt11)
	if err != nil {
		return false, payment, tries, err
	}

	hash := decoded.Get("payment_hash").String()
	exclude := []string{}
	payee := decoded.Get("payee").String()

	var msatoshi float64
	if imsatoshi, ok := params["msatoshi"]; ok {
		if converted, err := toFloat(imsatoshi); err == nil {
			msatoshi = converted
		}
	} else {
		msatoshi = decoded.Get("msatoshi").Float()
	}

	riskfactor, ok := params["riskfactor"]
	if !ok {
		riskfactor = 10
	}
	label, ok := params["label"]
	if !ok {
		label = ""
	}

	maxfeepercent := 0.5
	if imaxfeepercent, ok := params["maxfeepercent"]; ok {
		if converted, err := toFloat(imaxfeepercent); err == nil {
			maxfeepercent = converted
		}
	}
	exemptfee := 5000.0
	if iexemptfee, ok := params["exemptfee"]; ok {
		if converted, err := toFloat(iexemptfee); err == nil {
			exemptfee = converted
		}
	}

	fakePayment := gjson.Parse(`{"payment_hash": "` + hash + `"}`)
	for try := 0; try < 30; try++ {
		res, err := ln.CallNamed("getroute",
			"id", payee,
			"riskfactor", riskfactor,
			"cltv", 40,
			"msatoshi", msatoshi,
			"fuzzpercent", 0,
			"exclude", exclude,
		)
		if err != nil {
			// no route or invalid parameters, call it a simple failure
			return false, fakePayment, tries, nil
		}

		route := res.Get("route")
		if !route.Exists() {
			continue
		}

		// inspect route, it shouldn't be too expensive
		if route.Get("0.msatoshi").Float()/msatoshi > (1 + 1/maxfeepercent) {
			// too expensive, but we'll still accept it if the payment is small
			if msatoshi > exemptfee {
				// otherwise try the next route
				// we force that by excluding a channel
				exclude = append(exclude, getWorstChannel(route))
				continue
			}
		}

		// ignore returned value here as we'll get it from waitsendpay below
		_, err = ln.CallNamed("sendpay",
			"route", route.Value(), "payment_hash", hash, "label", label, "bolt11", bolt11,
		)
		if err != nil {
			// the command may return an error and we don't care
			if _, ok := err.(ErrorCommand); ok {
				// we don't care because we'll see this in the next call
			} else {
				// otherwise it's a different odd error, stop
				return false, fakePayment, tries, err
			}
		}

		// this should wait indefinitely, but 24h is enough
		res, err = ln.CallWithCustomTimeout(WaitSendPayTimeout, "waitsendpay", hash)
		if err != nil {
			if cmderr, ok := err.(ErrorCommand); ok {
				tries = append(tries, Try{route.Value(), &cmderr, false})

				if cmderr.Code == 200 || cmderr.Code == 202 || cmderr.Code == 204 {
					// try again
					continue
				}
			}

			// a different error, call it a complete failure
			return false, fakePayment, tries, err
		}

		// payment suceeded
		tries = append(tries, Try{route.Value(), nil, true})
		return true, res, tries, nil
	}

	// stopped trying
	return false, fakePayment, tries, nil
}

func getWorstChannel(route gjson.Result) (worstChannel string) {
	var worstFee int64 = 0
	hops := route.Array()
	if len(hops) == 1 {
		return hops[0].Get("channel").String() + "/" + hops[0].Get("direction").String()
	}

	for i := 0; i+1 < len(hops); i++ {
		hop := hops[i]
		next := hops[i+1]
		fee := hop.Get("msatoshi").Int() - next.Get("msatoshi").Int()
		if fee > worstFee {
			worstFee = fee
			worstChannel = hop.Get("channel").String() + "/" + hop.Get("direction").String()
		}
	}

	return
}

type Try struct {
	Route   interface{}   `json:"route"`
	Error   *ErrorCommand `json:"error"`
	Success bool          `json:"success"`
}
