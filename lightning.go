package lightning

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"time"
	"unicode"

	"github.com/tidwall/gjson"
)

const DefaultTimeout = time.Second * 5
const InvoiceListeningTimeout = time.Minute * 150

type Client struct {
	Path             string
	ErrorHandler     func(error)
	PaymentHandler   func(gjson.Result)
	LastInvoiceIndex int

	reqcount int
	waiting  map[string]chan gjson.Result
	conn     net.Conn
}

func Connect(path string) (*Client, error) {
	ln := &Client{
		Path: path,
		ErrorHandler: func(err error) {
			log.Print("lightning error: " + err.Error())
		},
		PaymentHandler: func(r gjson.Result) {
			log.Print("lightning payment: " + r.Get("label").String())
		},
	}
	ln.waiting = make(map[string]chan gjson.Result)

	err := ln.connect()
	if err != nil {
		return ln, err
	}

	ln.listen()
	return ln, nil
}

func (ln *Client) reconnect() {
	if ln.conn != nil {
		err := ln.conn.Close()
		if err != nil {
			log.Print("error closing old connection: " + err.Error())
		}
		ln.conn = nil
	}

	err := ln.connect()
	if err != nil {
		log.Print("error reconnecting: " + err.Error())
		log.Print("will try again.")
		time.Sleep(time.Second * 30)
		ln.reconnect()
		return
	}

	ln.listen()
	go ln.listenforinvoices()
}

func (ln *Client) connect() error {
	log.Print("connecting to " + ln.Path)
	conn, err := net.Dial("unix", ln.Path)
	if err != nil {
		return fmt.Errorf("Unable to dial socket %s:%s", ln.Path, err.Error())
	}

	ln.conn = conn
	return nil
}

func (ln *Client) listen() {
	errored := make(chan bool)

	go func() {
		for {
			if ln.conn == nil {
				break
			}

			message := make([]byte, 4096)
			length, err := ln.conn.Read(message)
			if err != nil {
				ln.ErrorHandler(err)
				errored <- true
				break
			}
			if length == 0 {
				continue
			}

			var messagerunes []byte
			for _, r := range bytes.Runes(message) {
				if unicode.IsGraphic(r) {
					messagerunes = append(messagerunes, byte(r))
				}
			}

			var response JSONRPCResponse
			err = json.Unmarshal(messagerunes, &response)
			if err != nil {
				log.Print("json decoding failed: " + err.Error())
				log.Print(string(messagerunes))
				ln.ErrorHandler(err)
				continue
			}

			if response.Error.Code != 0 {
				if response.Error.Code != 0 {
					err = errors.New(response.Error.Message)
				}
				ln.ErrorHandler(err)
				continue
			}

			if respchan, ok := ln.waiting[response.Id]; ok {
				log.Print("got response from lightningd: " + string(response.Result))
				respchan <- gjson.ParseBytes(response.Result)
				delete(ln.waiting, response.Id)
			} else {
				ln.ErrorHandler(
					errors.New("got response without a waiting caller: " +
						string(message)))
				continue
			}
		}
	}()

	go func() {
		select {
		case <-errored:
			log.Print("error break")

			// start again after an error break
			ln.reconnect()

			break
		}
	}()
}

func (ln *Client) listenforinvoices() {
	for {
		if ln.PaymentHandler == nil || ln.conn == nil {
			log.Print("stopped listening for invoices")
			return
		}

		res, err := ln.CallWithCustomTimeout(InvoiceListeningTimeout,
			"waitanyinvoice", strconv.Itoa(ln.LastInvoiceIndex))
		if err != nil {
			ln.ErrorHandler(err)
			time.Sleep(5 * time.Second)
			continue
		}

		index := res.Get("pay_index").Int()
		ln.LastInvoiceIndex = int(index)

		ln.PaymentHandler(res)
	}
}

func (ln *Client) ListenForInvoices() {
	go ln.listenforinvoices()
}

func (ln *Client) Call(method string, params ...string) (gjson.Result, error) {
	return ln.CallWithCustomTimeout(DefaultTimeout, method, params...)
}

func (ln *Client) CallWithCustomTimeout(timeout time.Duration, method string, params ...string) (gjson.Result, error) {

	id := strconv.Itoa(ln.reqcount)

	if params == nil {
		params = make([]string, 0)
	}

	message, _ := json.Marshal(JSONRPCMessage{
		Version: VERSION,
		Id:      id,
		Method:  method,
		Params:  params,
	})

	respchan := make(chan gjson.Result, 1)
	ln.waiting[id] = respchan

	log.Print("writing to lightningd: " + string(message))

	ln.reqcount++
	ln.conn.Write(message)

	select {
	case v := <-respchan:
		return v, nil
	case <-time.After(timeout):
		ln.reconnect()
		return gjson.Result{}, errors.New("timeout")
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
