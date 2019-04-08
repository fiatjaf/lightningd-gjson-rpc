package lightning

import (
	"errors"
	"log"
	"math"
	"time"

	"github.com/tidwall/gjson"
)

var InvoiceListeningTimeout = time.Minute * 150
var WaitPaymentMaxAttempts = 60

type Client struct {
	Path             string
	PaymentHandler   func(gjson.Result)
	LastInvoiceIndex int
}

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
				log.Printf("error waiting for invoice %d: %s", ln.LastInvoiceIndex, err.Error())
				time.Sleep(5 * time.Second)
				continue
			}

			index := res.Get("pay_index").Int()
			ln.LastInvoiceIndex = int(index)

			ln.PaymentHandler(res)
		}
	}()
}

// this function blocks
// call with the same parameters you would call "pay"
func (ln *Client) PayAndWaitUntilResolution(params ...interface{}) (success bool, payment gjson.Result, err error) {
	var bolt11 string

	if params == nil {
		return false, payment, errors.New("empty call")
	}

	switch firstparam := params[0].(type) {
	case map[string]interface{}:
		if vi, ok := firstparam["bolt11"]; ok {
			if v, ok := vi.(string); ok {
				bolt11 = v
			}
		}
	case interface{}:
		if v, ok := firstparam.(string); ok {
			bolt11 = v
		}
	}

	res, err := ln.Call("pay", params...)
	if err != nil {
		return ln.WaitPaymentResolution(bolt11)
	}

	return true, res, nil
}

// this function blocks
func (ln *Client) WaitPaymentResolution(bolt11 string) (success bool, payment gjson.Result, err error) {
	return ln.waitPaymentResolution(bolt11, 1)
}

func (ln *Client) waitPaymentResolution(bolt11 string, attempt int) (success bool, payment gjson.Result, err error) {
	if attempt > WaitPaymentMaxAttempts {
		return false, payment, errors.New("max payment confirmation attempts reached.")
	}
	time.Sleep(time.Second * time.Duration(int(math.Pow(1.1, float64(attempt)))))

	res, err := ln.Call("listpayments", bolt11)
	if err != nil {
		return false, payment, errors.New("failed to check payment status after the fact: " + err.Error())
	}
	payment = res.Get("payments.0")

	if payment.Exists() {
		switch payment.Get("status").String() {
		case "complete":
			return true, payment, nil
		case "failed":
			return false, payment, nil
		case "pending":
			return ln.waitPaymentResolution(bolt11, attempt+1)
		default:
			return false, payment,
				errors.New("payment in a weird state: " + payment.Get("status").String())
		}
	}

	return false, payment, errors.New("payment doesn't exist on `listpayments`")
}
