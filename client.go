package lightning

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

type Client struct {
	PaymentHandler   func(gjson.Result)
	LastInvoiceIndex int

	// lightning-rpc socket
	Path string

	// spark/sparko server
	SparkURL              string
	SparkToken            string
	DontCheckCertificates bool
}

// the lowest-level method for a socket client
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
			} else if response.Error != nil && response.Error.Code != 0 {
				errchan <- ErrorCommand{response.Error.Message, response.Error.Code, response.Error.Data}
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

// the lowest-level method for a spark client
func (ln *Client) callSpark(timeout time.Duration, body []byte) (res []byte, err error) {
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: ln.DontCheckCertificates},
		},
	}

	req, err := http.NewRequest("POST", ln.SparkURL, bytes.NewBuffer(body))
	if err != nil {
		err = ErrorConnect{ln.SparkURL, err.Error()}
		return
	}
	if ln.SparkToken != "" {
		req.Header.Add("X-Access", ln.SparkToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		if strings.Index(err.Error(), "imeout") != -1 {
			err = ErrorTimeout{int(timeout.Seconds())}
			return
		}

		err = ErrorConnect{ln.SparkURL, err.Error()}
		return
	}

	if resp.StatusCode >= 300 {
		var sparkerr JSONRPCError
		err = json.NewDecoder(resp.Body).Decode(&sparkerr)
		if err != nil {
			return
		}
		return nil, ErrorCommand{sparkerr.Message, sparkerr.Code, sparkerr.Data}
	}

	res, err = ioutil.ReadAll(resp.Body)
	return
}
