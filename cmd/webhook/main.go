package main

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"

	"github.com/fiatjaf/lightningd-gjson-rpc"
	"github.com/tidwall/gjson"
)

var ln *lightning.Client
var webhookURL string

func plog(str string) {
	log.Print("plugin-webhook " + str)
}

const manifest = `{
  "options": [{
    "name": "webhook",
    "type": "string",
    "description": "To which URL should we send incoming payment notifications?"
  }],
  "rpcmethods": [],
  "subscriptions": []
}`

func paymentHandler(inv gjson.Result) {
	invv := inv.Value()
	jinv, err := json.Marshal(invv)
	if err != nil {
		plog("invalid invoice gotten from lightningd: " + inv.String())
		return
	}
	req, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(jinv))
	if err != nil {
		plog("failed to send webhook to " + webhookURL + ": " + err.Error())
		return
	}
	if req.StatusCode >= 300 {
		b, _ := ioutil.ReadAll(req.Body)
		plog(
			"webhook handler at " + webhookURL +
				" returned error (" + strconv.Itoa(req.StatusCode) + "): " + string(b),
		)
		return
	}
}

func main() {
	var msg lightning.JSONRPCMessage

	incoming := json.NewDecoder(os.Stdin)
	outgoing := json.NewEncoder(os.Stdout)
	for {
		err := incoming.Decode(&msg)
		if err == io.EOF {
			return
		}

		response := lightning.JSONRPCResponse{
			Version: msg.Version,
			Id:      msg.Id,
		}

		if err != nil {
			plog("failed to decode request, killing: " + err.Error())
			return
		}

		switch msg.Method {
		case "init":
			init := msg.Params.(map[string]interface{})

			// get webhook URL
			ioptions := init["options"]
			options := ioptions.(map[string]interface{})
			if u, ok := options["webhook"]; ok && u != "" {
				webhookURL = u.(string)
				if _, err := url.Parse(webhookURL); err != nil {
					plog("invalid URL (" + webhookURL + ") passed to `webhook` option: " + err.Error())
					return
				}
			} else {
				plog("`webhook` option not passed, can't initialize webhook plugin.")
				return
			}

			// init client
			iconf := init["configuration"]
			conf := iconf.(map[string]interface{})
			ilnpath := conf["lightning-dir"]
			irpcfile := conf["rpc-file"]
			rpc := path.Join(ilnpath.(string), irpcfile.(string))
			ln = &lightning.Client{
				Path:             rpc,
				LastInvoiceIndex: 0,
				PaymentHandler:   paymentHandler,
			}

			// get latest invoice index and start listening from it
			res, err := ln.Call("listinvoices")
			if err != nil {
				plog("failed to get last invoice index for webhook plugin: %s" + err.Error())
				return
			}
			indexes := res.Get("invoices.#.pay_index").Array()
			for _, indexr := range indexes {
				index := int(indexr.Int())
				if index > ln.LastInvoiceIndex {
					ln.LastInvoiceIndex = index
				}
			}

			// start listening
			ln.ListenForInvoices()

			plog("initialized webhook plugin. listening from pay_index " +
				strconv.Itoa(ln.LastInvoiceIndex) +
				". sending webhooks to " + webhookURL + ".",
			)
		case "getmanifest":
			json.Unmarshal([]byte(manifest), &response.Result)
		}

		outgoing.Encode(response)
	}
}
