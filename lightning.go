package lightning

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"

	"github.com/tidwall/gjson"
)

func (ln *Client) Call(method string, params ...interface{}) (gjson.Result, error) {
	timeout := ln.CallTimeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	return ln.CallWithCustomTimeout(timeout, method, params...)
}

func (ln *Client) CallNamed(method string, params ...interface{}) (gjson.Result, error) {
	timeout := ln.CallTimeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	return ln.CallNamedWithCustomTimeout(timeout, method, params...)
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
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	encoder.Encode(message)
	mbytes := buffer.Bytes()

	if ln.Path != "" {
		// it's a socket client
		return ln.callMessageBytes(timeout, 0, mbytes)
	} else if ln.SparkURL != "" {
		// it's a spark client
		return ln.callSpark(timeout, mbytes)
	} else {
		return nil, errors.New("misconfigured client: missing Path or SparkURL.")
	}
}

const version = "2.0"

type JSONRPCMessage struct {
	Version string      `json:"jsonrpc"`
	Id      interface{} `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

type JSONRPCResponse struct {
	Version string          `json:"jsonrpc"`
	Id      interface{}     `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}
