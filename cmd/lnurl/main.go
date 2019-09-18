package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"

	"github.com/btcsuite/btcd/btcec"
	"github.com/fiatjaf/go-lnurl"
	"github.com/fiatjaf/lightningd-gjson-rpc"
	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
	"github.com/tidwall/gjson"
)

var ln *lightning.Client

func plog(str string) {
	log.Print("plugin-lnurl " + str)
}

var usage = "lnurl [private] [msatoshi] [description]"

var manifest = `{
  "options": [],
  "rpcmethods": [
    {
      "name": "lnurlencode",
      "usage": "url",
      "description": "Will encode an URL as bech32 with the 'lnurl' prefix. A small helper for servers that want to implement the server-side of the lnurl flow. {lnurl} is the bech32-encoded URL to query."
    },
    {
      "name": "lnurlparams",
      "usage": "lnurl",
      "description": "Will fetch the params from the server or (when the decoded URL has a 'login' querystring) get then from the querystring, then return these params as JSON. {lnurl} is the bech32-encoded URL to query."
    },
    {
      "name": "lnurl",
      "usage": "` + usage + `",
      "description": "Will decode the lnurl, get its params (as in 'lnurlparams') and proceed with the lnurl flow according to the tag (login, withdraw etc.). {lnurl} is the bech32-encoded URL to query. {private} is either true or false, used on lnurl-channel for the type of channel (defaults to false). {description} is used on lnurl-withdraw (defaults to the default description). {msatoshi} is an integer, used on lnurl-withdraw and lnurl-pay (defaults to maximum possible amount)."
    }
  ],
  "subscriptions": []
}`

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
			iconf := msg.Params.(map[string]interface{})["configuration"]
			conf := iconf.(map[string]interface{})
			ilnpath := conf["lightning-dir"]
			irpcfile := conf["rpc-file"]
			rpc := path.Join(ilnpath.(string), irpcfile.(string))
			ln = &lightning.Client{Path: rpc}
			plog("initialized lnurl plugin.")
		case "getmanifest":
			json.Unmarshal([]byte(manifest), &response.Result)
		case "lnurlencode":
			params, err := plugin.GetParams(msg, "url")
			if err != nil {
				response.Error = &lightning.JSONRPCError{
					Code:    400,
					Message: err.Error(),
				}
				goto end
			}

			url, _ := params["url"].(string)
			encodedlnurl, err := lnurl.LNURLEncode(url)
			if err != nil {
				response.Error = &lightning.JSONRPCError{
					Code:    500,
					Message: err.Error(),
				}
				goto end
			}

			json.Unmarshal([]byte(`{"lnurl": "`+encodedlnurl+`"}`), &response.Result)
		case "lnurlparams":
			params, err := plugin.GetParams(msg, "lnurl")
			if err != nil {
				response.Error = &lightning.JSONRPCError{
					Code:    400,
					Message: err.Error(),
				}
				goto end
			}

			data, err := lnurl.HandleLNURL(params["lnurl"].(string))
			if err != nil {
				response.Error = &lightning.JSONRPCError{
					Code:    401,
					Message: err.Error(),
				}
				goto end
			}

			j, _ := json.Marshal(data)
			json.Unmarshal(j, &response.Result)
		case "lnurl":
			params, err := plugin.GetParams(msg, usage)
			if err != nil {
				response.Error = &lightning.JSONRPCError{
					Code:    400,
					Message: err.Error(),
				}
				goto end
			}

			data, err := lnurl.HandleLNURL(params["lnurl"].(string))
			if err != nil {
				response.Error = &lightning.JSONRPCError{
					Code:    401,
					Message: err.Error(),
				}
				goto end
			}

			switch lnurlparams := data.(type) {
			case lnurl.LNURLChannelResponse:
				// connect to target node, notify it and wait for incoming channel.
				// no user interaction needed.
				private, _ := params["private"]
				var privateparam string
				switch priv := private.(type) {
				case bool:
					if priv {
						privateparam = "1"
					} else {
						privateparam = "0"
					}
				default:
					privateparam = "0"
				}

				_, err = ln.Call("connect", lnurlparams.URI)
				if err != nil {
					response.Error = &lightning.JSONRPCError{
						Code:    204,
						Message: err.Error(),
					}
					goto end
				}

				respinfo, err := ln.Call("getinfo")
				if err != nil {
					response.Error = &lightning.JSONRPCError{
						Code:    501,
						Message: err.Error(),
					}
					goto end
				}

				u, err := url.Parse(lnurlparams.Callback)
				if err != nil {
					response.Error = &lightning.JSONRPCError{
						Code:    202,
						Message: err.Error(),
					}
					goto end
				}

				qs := u.Query()
				qs.Set("k1", lnurlparams.K1)
				qs.Set("private", privateparam)
				qs.Set("remoteid", respinfo.Get("id").String())
				u.RawQuery = qs.Encode()

				callCallback(u, &response, map[string]interface{}{
					"status":              "OK",
					"waiting_for_channel": true,
				})
			case lnurl.LNURLLoginParams:
				// sign k1 with linkingKey and send it along with key
				k1, err := hex.DecodeString(lnurlparams.K1)
				if err == nil && len(k1) != 32 {
					err = errors.New("Invalid length k1.")
				}
				if err != nil {
					response.Error = &lightning.JSONRPCError{
						Code:    407,
						Message: err.Error(),
					}
					goto end
				}

				// parse callback url
				u, err := url.Parse(lnurlparams.Callback)
				if err != nil {
					response.Error = &lightning.JSONRPCError{
						Code:    202,
						Message: err.Error(),
					}
					goto end
				}

				// get linking key for callback domain
				sk, err := getLinkingKey(u.Host)
				if err != nil {
					response.Error = &lightning.JSONRPCError{
						Code:    508,
						Message: err.Error(),
					}
					goto end
				}

				// sign
				sig, err := sk.Sign(k1)
				if err != nil {
					response.Error = &lightning.JSONRPCError{
						Code:    509,
						Message: err.Error(),
					}
					goto end
				}

				// call callback with signature and key
				signature := hex.EncodeToString(sig.Serialize())
				pubkey := hex.EncodeToString(sk.PubKey().SerializeCompressed())
				qs := u.Query()
				qs.Set("sig", signature)
				qs.Set("key", pubkey)
				u.RawQuery = qs.Encode()

				callCallback(u, &response, map[string]interface{}{
					"status":     "OK",
					"login":      true,
					"domain":     u.Host,
					"public_key": pubkey,
					"signature":  signature,
				})
			case lnurl.LNURLWithdrawResponse:
				// amount and description should be taken either from CLI params
				// or from interactive input.

				description := lnurlparams.DefaultDescription
				msatoshi := getBiggestIncomingCapacity()

				if msatoshi > lnurlparams.MaxWithdrawable {
					msatoshi = lnurlparams.MaxWithdrawable
				}

				idescription, ok := params["description"]
				if ok {
					descriptionparam, _ := idescription.(string)
					if descriptionparam != "" {
						description = descriptionparam
					}
				}

				imsatoshi, ok := params["msatoshi"]
				if ok {
					msatoshiparam, _ := imsatoshi.(int)
					if msatoshiparam != 0 {
						msatoshi = int64(msatoshiparam)

						// check msatoshi min/max
						if msatoshi > lnurlparams.MaxWithdrawable {
							response.Error = &lightning.JSONRPCError{
								Code:    507,
								Message: fmt.Sprintf("msatoshi amount '%d' is bigger than the maximum specified by the server (%d).", msatoshi, lnurlparams.MaxWithdrawable),
							}
							goto end
						}
						if msatoshi > lnurlparams.MinWithdrawable {
							response.Error = &lightning.JSONRPCError{
								Code:    507,
								Message: fmt.Sprintf("msatoshi amount '%d' is smaller than the minimum specified by the server (%d).", msatoshi, lnurlparams.MinWithdrawable),
							}
							goto end
						}
						if msatoshi > getBiggestIncomingCapacity() {
							response.Error = &lightning.JSONRPCError{
								Code:    507,
								Message: fmt.Sprintf("msatoshi amount '%d' is bigger than the maximum we can receive (%d).", msatoshi, getBiggestIncomingCapacity()),
							}
							goto end
						}
					}
				}

				// parse callback
				u, err := url.Parse(lnurlparams.Callback)
				if err != nil {
					response.Error = &lightning.JSONRPCError{
						Code:    202,
						Message: err.Error(),
					}
					goto end
				}

				// generate bolt11 invoice
				label := fmt.Sprintf("lnurl-withdraw-%s-%s", u.Host, lnurlparams.K1)
				resp, err := ln.Call("invoice", msatoshi, label, description)
				bolt11 := resp.Get("bolt11").String()

				// call callback with bolt11 invoice and params
				qs := u.Query()
				qs.Set("k1", lnurlparams.K1)
				qs.Set("pr", bolt11)

				// only if there's a valid k1, sign it too
				if k1, err := hex.DecodeString(lnurlparams.K1); err == nil {
					if sk, err := getLinkingKey(u.Host); err == nil {
						if sig, err := sk.Sign(k1); err == nil {
							qs.Set("sig", hex.EncodeToString(sig.Serialize()))
						}
					}
				} // otherwise ignore.

				u.RawQuery = qs.Encode()

				callCallback(u, &response, map[string]interface{}{
					"status":              "OK",
					"waiting_for_payment": true,
					"bolt11":              bolt11,
					"label":               label,
				})
			case lnurl.LNURLPayResponse:
			}
		}

	end:
		outgoing.Encode(response)
	}
}

func getBiggestIncomingCapacity() (biggest int64) {
	resp, err := ln.Call("listfunds")
	if err != nil {
		return
	}

	resp.Get("channels").ForEach(func(_, value gjson.Result) bool {
		incoming := value.Get("channel_total_sat").Int() - value.Get("channel_sat").Int()
		if incoming > biggest {
			biggest = incoming
		}
		return true
	})

	return biggest
}

func callCallback(
	u *url.URL,
	response *lightning.JSONRPCResponse,
	jsonresponsesuccess map[string]interface{},
) {
	resp, err := http.Get(u.String())
	if err != nil {
		response.Error = &lightning.JSONRPCError{
			Code:    501,
			Message: err.Error(),
		}
		return
	}

	var lnurlresp lnurl.LNURLResponse
	err = json.NewDecoder(resp.Body).Decode(&lnurlresp)
	if err != nil {
		response.Error = &lightning.JSONRPCError{
			Code:    205,
			Message: err.Error(),
		}
		return
	}

	if lnurlresp.Status == "ERROR" {
		response.Error = &lightning.JSONRPCError{
			Code:    206,
			Message: lnurlresp.Reason,
		}
		return
	}

	j, _ := json.Marshal(jsonresponsesuccess)
	json.Unmarshal(j, &response.Result)
}

func getLinkingKey(domain string) (sk *btcec.PrivateKey, err error) {
	return ln.GetCustomKey(138, domain)
}
