package lightning

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/tidwall/gjson"
)

const DefaultTimeout = time.Second * 5
const InvoiceListeningTimeout = time.Minute * 150

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
		if retrySequence < 6 {
			time.Sleep(time.Second * 2 * (time.Duration(retrySequence) + 1))
			return ln.callWithCustomTimeoutAndRetry(timeout, retrySequence+1, method, params...)
		} else {
			err = ErrorConnect{ln.Path, err.Error()}
			return
		}
	}
	defer conn.Close()

	message, _ := json.Marshal(jsonrpcmessage{
		Version: version,
		Id:      "0",
		Method:  method,
		Params:  sparams,
	})

	respchan := make(chan gjson.Result)
	errchan := make(chan error)
	go func() {
		decoder := json.NewDecoder(conn)
		for {
			var response jsonrpcresponse
			err := decoder.Decode(&response)
			if err == io.EOF {
				errchan <- ErrorConnectionBroken{}
				break
			} else if err != nil {
				errchan <- ErrorJSONDecode{err.Error()}
				break
			} else if response.Error.Code != 0 {
				errchan <- ErrorCommand{response.Error.Message, response.Error.Code}
				break
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
		err = ErrorTimeout{int(timeout.Seconds())}
		return
	}
}

const version = "2.0"

type jsonrpcmessage struct {
	Version string   `json:"jsonrpc"`
	Id      string   `json:"id"`
	Method  string   `json:"method"`
	Params  []string `json:"params"`
}

type jsonrpcresponse struct {
	Version string          `json:"jsonrpc"`
	Id      string          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type ErrorConnect struct {
	Path string
	Msg  string
}

type ErrorCommand struct {
	Msg  string
	Code int
}

type ErrorTimeout struct {
	Seconds int
}

type ErrorJSONDecode struct {
	Msg string
}

type ErrorConnectionBroken struct{}

func (c ErrorConnect) Error() string {
	return fmt.Sprintf("unable to dial socket %s:%s", c.Path, c.Msg)
}
func (l ErrorCommand) Error() string {
	return fmt.Sprintf("lightningd replied with error: %s (%d)", l.Msg, l.Code)
}
func (t ErrorTimeout) Error() string {
	return fmt.Sprintf("call timed out after %ds", t.Seconds)
}
func (j ErrorJSONDecode) Error() string {
	return "error decoding JSON response from lightningd: " + j.Msg
}
func (c ErrorConnectionBroken) Error() string {
	return "got an EOF while reading response, it seems the connection is broken"
}
