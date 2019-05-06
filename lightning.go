package lightning

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"time"

	"github.com/kr/pretty"
	"github.com/tidwall/gjson"
)

var DefaultTimeout = time.Second * 5

func (ln *Client) Call(method string, params ...interface{}) (gjson.Result, error) {
	return ln.CallWithCustomTimeout(DefaultTimeout, method, params...)
}

func (ln *Client) CallNamed(method string, params ...interface{}) (gjson.Result, error) {
	return ln.CallNamedWithCustomTimeout(DefaultTimeout, method, params...)
}

func (ln *Client) CallNamedWithCustomTimeout(
	timeout time.Duration,
	method string,
	params ...interface{},
) (res gjson.Result, err error) {
	if len(params)%2 != 0 {
		err = errors.New("Wrong number of parameters.")
		return
	}

	named := make(map[string]interface{})
	for i := 0; i < len(params); i += 2 {
		if key, ok := params[i].(string); ok {
			value := params[i+1]
			named[key] = value
		}
	}

	return ln.CallWithCustomTimeout(timeout, method, named)
}

func (ln *Client) CallWithCustomTimeout(
	timeout time.Duration,
	method string,
	params ...interface{},
) (gjson.Result, error) {
	var payload interface{}
	var sparams []interface{}

	if params == nil {
		payload = make([]string, 0)
		goto gotpayload
	}

	if len(params) == 1 {
		if named, ok := params[0].(map[string]interface{}); ok {
			payload = named
			goto gotpayload
		}
	}

	sparams = make([]interface{}, len(params))
	for i, iparam := range params {
		sparams[i] = iparam
	}
	payload = sparams

gotpayload:
	message := JSONRPCMessage{
		Version: version,
		Method:  method,
		Params:  payload,
	}

	pretty.Log(message)

	return ln.CallMessage(timeout, message)
}

func (ln *Client) CallMessage(timeout time.Duration, message JSONRPCMessage) (gjson.Result, error) {
	bres, err := ln.CallMessageRaw(timeout, message)
	if err != nil {
		return gjson.Result{}, err
	}
	return gjson.ParseBytes(bres), nil
}

func (ln *Client) CallMessageRaw(timeout time.Duration, message JSONRPCMessage) ([]byte, error) {
	message.Id = "0"
	if message.Params == nil {
		message.Params = make([]string, 0)
	}
	mbytes, _ := json.Marshal(message)
	return ln.callMessageBytes(timeout, 0, mbytes)
}

func (ln *Client) callMessageBytes(
	timeout time.Duration,
	retrySequence int,
	message []byte,
) (res []byte, err error) {
	conn, err := net.Dial("unix", ln.Path)
	if err != nil {
		if retrySequence < 6 {
			time.Sleep(time.Second * 2 * (time.Duration(retrySequence) + 1))
			return ln.callMessageBytes(timeout, retrySequence+1, message)
		} else {
			err = ErrorConnect{ln.Path, err.Error()}
			return
		}
	}
	defer conn.Close()

	respchan := make(chan []byte)
	errchan := make(chan error)
	go func() {
		decoder := json.NewDecoder(conn)
		for {
			var response JSONRPCResponse
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
			respchan <- response.Result
		}
	}()

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

type JSONRPCMessage struct {
	Version string      `json:"jsonrpc"`
	Id      string      `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
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
