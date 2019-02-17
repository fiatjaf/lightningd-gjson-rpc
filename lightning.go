package lightning

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/tidwall/gjson"
)

const DefaultTimeout = time.Second * 5
const InvoiceListeningTimeout = time.Minute * 150

var TimeoutError = errors.New("--timeout--")

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

func (ln *Client) Call(method string, params ...interface{}) (gjson.Result, error) {
	return ln.CallWithCustomTimeout(DefaultTimeout, method, params...)
}

func (ln *Client) CallWithCustomTimeout(
	timeout time.Duration,
	method string,
	params ...interface{},
) (res gjson.Result, err error) {
	return ln.callWithCustomTimeoutAndRetry(timeout, 0, method, params...)
}

func (ln *Client) callWithCustomTimeoutAndRetry(
	timeout time.Duration,
	retrySequence int,
	method string,
	params ...interface{},
) (res gjson.Result, err error) {
	var sparams []string
	if params == nil {
		sparams = make([]string, 0)
	} else {
		sparams = make([]string, len(params))
		for i, iparam := range params {
			sparams[i] = fmt.Sprintf("%v", iparam)
		}
	}

	conn, err := net.Dial("unix", ln.Path)
	if err != nil {
		if retrySequence < 10 {
			return ln.callWithCustomTimeoutAndRetry(timeout, retrySequence+1, method, params...)
		} else {
			err = fmt.Errorf("Unable to dial socket %s:%s", ln.Path, err.Error())
			return
		}
	}
	defer conn.Close()

	message, _ := json.Marshal(JSONRPCMessage{
		Version: VERSION,
		Id:      "0",
		Method:  method,
		Params:  sparams,
	})

	respchan := make(chan gjson.Result)
	errchan := make(chan error)
	go func() {
		decoder := json.NewDecoder(conn)
		for {
			var response JSONRPCResponse
			err := decoder.Decode(&response)
			if err == io.EOF {
				errchan <- err
				break
			} else if err != nil {
				errchan <- err
				break
			} else if response.Error.Code != 0 {
				errchan <- fmt.Errorf("lightningd replied with error: %s (%d)",
					response.Error.Message, response.Error.Code)
			}
			respchan <- gjson.ParseBytes(response.Result)
		}
	}()

	log.Print("writing to lightningd: " + string(message))
	conn.Write(message)

	select {
	case v := <-respchan:
		return v, nil
	case err = <-errchan:
		return
	case <-time.After(timeout):
		err = TimeoutError
		return
	}
}

const VERSION = "2.0"

type JSONRPCMessage struct {
	Version string   `json:"jsonrpc"`
	Id      string   `json:"id"`
	Method  string   `json:"method"`
	Params  []string `json:"params"`
}

type JSONRPCResponse struct {
	Version string          `json:"jsonrpc"`
	Id      string          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}
