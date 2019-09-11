package main

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path"
	"time"

	"github.com/fiatjaf/lightningd-gjson-rpc"
	"github.com/tidwall/gjson"
)

var ln *lightning.Client
var logs = make(map[string][]lightning.Try)

func plog(str string) {
	log.Print("plugin-waitpay " + str)
}

const manifest = `{
  "options": [],
  "rpcmethods": [
    {
      "name": "waitpay",
      "usage": "bolt11 [msatoshi] [riskfactor] [label] [maxfeepercent] [exemptfee] [description]",
      "description": "Tries to pay the given invoice and blocks until it is finally paid or errored. Params mean the same as the 'pay' method, except {description}, which is the description preimage needed in case {bolt11} was encoded with a 'description_hash'."
    },
    {
      "name": "waitpaystatus",
      "usage": "bolt11",
      "description": "Get data on the last payment attempt for the given invoice."
    }
  ],
  "subscriptions": []
}`

var waitpaykeys []string = []string{"bolt11", "msatoshi", "riskfactor", "label", "maxfeepercent", "exemptfee", "description"}
var waitpaystatuskeys []string = []string{"bolt11"}

func main() {
	lightning.WaitSendPayTimeout = time.Hour * 240

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
			iconf := msg.Params.(map[string]interface{})["configuration"]
			conf := iconf.(map[string]interface{})
			ilnpath := conf["lightning-dir"]
			irpcfile := conf["rpc-file"]
			rpc := path.Join(ilnpath.(string), irpcfile.(string))
			ln = &lightning.Client{Path: rpc}
			plog("initialized waitpay plugin.")
		case "getmanifest":
			json.Unmarshal([]byte(manifest), &response.Result)
		case "waitpay":
			var bolt11 string
			params := make(map[string]interface{})

			switch p := msg.Params.(type) {
			case []interface{}:
				for i, v := range p {
					if i < len(waitpaykeys) { // ignore extra parameters
						params[waitpaykeys[i]] = v
					}
				}
			case map[string]interface{}:
				params = p
			}

			// grab bolt11 from parameters map
			ibolt11, ok := params["bolt11"]
			if !ok {
				goto missingbolt11
			}
			bolt11, ok = ibolt11.(string)
			if !ok {
				goto missingbolt11
			}
			if bolt11 == "" {
				goto missingbolt11
			}

			var (
				success bool
				payment gjson.Result
				tries   []lightning.Try
			)
			success, payment, tries, err = ln.PayAndWaitUntilResolution(bolt11, params)
			if err != nil {
				goto paymentcallerr
			}

			// cache this here for future calls
			logs[bolt11] = tries

			if !success {
				// in this case we fetch the failed payment object from lightningd for consistency
				var res gjson.Result
				res, err = ln.Call("listpayments", bolt11)
				if err != nil {
					goto listpaymentserr
				}
				if res.Get("payments.#").Int() == 0 {
					goto noteventried
				}
				payment = res.Get("payments.0")
			}

			// return the payment object
			json.Unmarshal([]byte(payment.String()), &response.Result)
			goto end

		case "waitpaystatus":
			var bolt11 string
			params := make(map[string]interface{})

			switch p := msg.Params.(type) {
			case []interface{}:
				for i, v := range p {
					if i < len(waitpaystatuskeys) { // ignore extra parameters
						params[waitpaystatuskeys[i]] = v
					}
				}
			case map[string]interface{}:
				params = p
			}

			// grab bolt11 from parameters map
			ibolt11, ok := params["bolt11"]
			if !ok {
				goto missingbolt11
			}
			bolt11, ok = ibolt11.(string)
			if !ok {
				goto missingbolt11
			}
			if bolt11 == "" {
				goto missingbolt11
			}

			tries, _ := logs[bolt11]
			result, _ := json.Marshal(tries)
			json.Unmarshal(result, &response.Result)
			goto end
		}

	missingbolt11:
		response.Error = &lightning.JSONRPCError{
			Code:    -2,
			Message: "Missing bolt11 invoice.",
		}
		goto end
	paymentcallerr:
		response.Error = &lightning.JSONRPCError{
			Code:    -12,
			Message: "Unexpected error: '" + err.Error() + "'",
		}
		goto end
	listpaymentserr:
		response.Error = &lightning.JSONRPCError{
			Code:    212,
			Message: "Payment failed: '" + err.Error() + "'",
		}
		goto end
	noteventried:
		response.Error = &lightning.JSONRPCError{
			Code:    208,
			Message: "Payment not even tried: '" + err.Error() + "'",
		}
		goto end

	end:
		outgoing.Encode(response)
	}
}
