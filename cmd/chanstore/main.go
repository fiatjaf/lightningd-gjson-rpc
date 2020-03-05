package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/bbolt"
	lightning "github.com/fiatjaf/lightningd-gjson-rpc"
	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
	"github.com/lucsky/cuid"
)

const (
	DATABASE_FILE = "chanstore.db"

	REPLY_INVOICE       = 63241
	REPLY_CHANNEL       = 63243
	MSG_REQUEST_INVOICE = 63245
	MSG_ADD_CHANNEL     = 63247
	MSG_REPORT_CHANNEL  = 63249
	MSG_REQUEST_CHANNEL = 63251

	PROBE_AMOUNT = 100000
)

var (
	continuehook = map[string]string{"result": "continue"}
	multipliers  = map[string]float64{
		"msat": 1,
		"sat":  1000,
		"btc":  100000000000,
	}
	channelswaitingtosend = map[string]*lightning.Channel{}
)

var db *bbolt.DB
var err error

func main() {
	p := plugin.Plugin{
		Name:    "chanstore",
		Version: "v0.1",
		Dynamic: true,
		Options: []plugin.Option{
			{
				"chanstore-connect",
				"string",
				"",
				"chanstore service addresses to fetch channels from, comma-separated.",
			},
			{
				"chanstore-price",
				"integer",
				72,
				"Satoshi price to ask for peers to include a channel.",
			},
			{
				"chanstore-connect",
				"string",
				"",
				"chanstore services to fetch channels from, comma-separated.",
			},
		},
		Hooks: []plugin.Hook{
			{
				"rpc_command",
				func(p *plugin.Plugin, payload plugin.Params) (resp interface{}) {
					rpc_command := payload.Get("rpc_command.rpc_command")

					if rpc_command.Get("method").String() != "getroute" {
						return continuehook
					}

					iparams := rpc_command.Get("params").Value()
					var parsed plugin.Params
					switch params := iparams.(type) {
					case []interface{}:
						parsed, err = plugin.GetParams(
							params,
							"id msatoshi riskfactor [cltv] [fromid] [fuzzpercent] [exclude] [maxhops]",
						)
						if err != nil {
							p.Log("failed to parse getroute parameters: %s", err)
							return continuehook
						}
					case map[string]interface{}:
						parsed = plugin.Params(params)
					}

					exc := parsed.Get("exclude").Array()
					exclude := make([]string, len(exc))
					for i, chandir := range exc {
						exclude[i] = chandir.String()
					}

					fuzz := parsed.Get("fuzzpercent").String()
					fuzzpercent, err := strconv.ParseFloat(fuzz, 64)
					if err != nil {
						fuzzpercent = 5.0
					}

					fromid := parsed.Get("fromid").String()
					if fromid == "" {
						res, err := p.Client.Call("getinfo")
						if err != nil {
							p.Logf("can't get our own nodeid: %w", err)
							return continuehook
						}
						fromid = res.Get("id").String()
					}

					cltv := parsed.Get("cltv").Int()
					if cltv == 0 {
						cltv = 9
					}

					maxhops := int(parsed.Get("maxhops").Int())
					if maxhops == 0 {
						maxhops = 20
					}

					msatoshi := parsed.Get("msatoshi").Int()
					if msatoshi == 0 {
						for _, suffix := range []string{"msat", "sat", "btc"} {
							msatoshiStr := parsed.Get("msatoshi").String()
							spl := strings.Split(msatoshiStr, suffix)
							if len(spl) == 2 {
								amt, err := strconv.ParseFloat(spl[0], 10)
								if err != nil {
									return map[string]interface{}{
										"return": map[string]interface{}{
											"error": "failed to parse " + msatoshiStr,
										},
									}
								}
								msatoshi = int64(amt * multipliers[suffix])
								break
							}
						}
					}

					if msatoshi == 0 {
						return map[string]interface{}{
							"return": map[string]interface{}{
								"error": "msatoshi can't be 0",
							},
						}
					}

					target := parsed.Get("id").String()
					riskfactor := parsed.Get("riskfactor").Int()

					p.Logf("querying route from %s to %s for %d msatoshi with riskfactor %d, fuzzpercent %f, excluding %v", fromid, target, msatoshi, riskfactor, fuzzpercent, exclude)

					route, err := p.Client.GetRoute(
						target,
						msatoshi,
						riskfactor,
						cltv,
						fromid,
						fuzzpercent,
						exclude,
						maxhops,
						0.5,
					)

					if err != nil {
						p.Logf("failed to getroute: %s, falling back to default.", err)
						return continuehook
					}

					return map[string]interface{}{
						"return": map[string]interface{}{
							"result": map[string]interface{}{
								"route": route,
							},
						},
					}
				},
			},
			{
				"custommsg",
				func(p *plugin.Plugin, payload plugin.Params) (resp interface{}) {
					peer := payload.Get("peer_id").String()
					message := payload.Get("message").String()
					resp = continuehook

					code, err := strconv.ParseInt(message[:4], 16, 64)
					if err != nil {
						p.Logf("got invalid custommsg: %s (%s)", message, err.Error())
						return
					}

					switch code {
					case MSG_REQUEST_INVOICE:
						// peer wants to add a channel to our database, send an invoice
						res, err := p.Client.Call("invoice", p.Args.Get("chanstore-price").String()+"sat", "chanstore/"+peer, "Ticket to include a channel on chanstore.")
						if err != nil {
							p.Logf("error creating invoice: %s", err.Error())
							return
						}
						bolt11 := res.Get("bolt11").String()

						_, err = p.Client.Call("dev-sendcustommsg", peer, strconv.FormatInt(REPLY_INVOICE, 16)+hex.EncodeToString([]byte(bolt11)))
						if err != nil {
							p.Logf("error sending reply: %s", err.Error())
							return
						}
					case MSG_ADD_CHANNEL:
						// receive a channel to be added to the database
						var channel lightning.Channel
						channeldata, _ := hex.DecodeString(message[4:])
						err := json.Unmarshal(channeldata, &channel)
						if err != nil {
							p.Logf("invalid channel-add message: %s", err.Error())
							return
						}

						// check if invoice has been paid
						res, _ := p.Client.Call("listchannels", "chanstore/"+peer)
						if res.Get("invoices.0.status").String() != "paid" {
							p.Logf("channel-add, but chanstore/%s is not paid", peer)
							return
						}

						// delete invoice so the user can request a new one later
						p.Client.Call("delinvoice", "chanstore/"+peer, "paid")

						p.Log("adding channel: ", channel)

						// check if channel really exists and can route
						// by sending a circular payment to us passing through that
						res, _ = p.Client.Call("invoice", PROBE_AMOUNT,
							cuid.Slug(), "chanstore probe")
						bolt11, _ := p.Client.Call("decodepay",
							res.Get("bolt11").String())
						hash := bolt11.Get("payment_hash").String()
						we := bolt11.Get("payee").String()
						cltv := bolt11.Get("min_final_cltv_expiry").Int()
						exclude := make([]string, 0, 20)

						for i := 0; i < 30; i++ {
							path1, err := p.Client.GetPath(
								channel.Source, PROBE_AMOUNT, we, exclude, 20, 0.5)
							if err != nil {
								p.Logf("channel-add route failure: %s", err.Error())
								return
							}
							path2, err := p.Client.GetPath(
								we, PROBE_AMOUNT, channel.Destination, exclude, 20, 0.5)
							if err != nil {
								p.Logf("channel-add route failure: %s", err.Error())
								return
							}
							path := append(path1, &channel)
							path = append(path, path2...)
							route := lightning.PathToRoute(path, PROBE_AMOUNT, cltv, 0, 0)

							// naïve maxfeepercent
							if route[0].Msatoshi > PROBE_AMOUNT*1.01 {
								continue
							}

							p.Client.Call("sendpay", route, hash)
							_, err = p.Client.Call("waitsendpay", hash)
							if err != nil {
								if errc, ok := err.(lightning.ErrorCommand); ok {
									d, ok := errc.Data.(map[string]interface{})
									if !ok {
										p.Logf("probe payment failed: %s", err.Error())
										return
									}

									if _, ok := d["erring_direction"]; !ok {
										p.Logf("probe payment failed: %s", err.Error())
										return
									}

									// naïvely exclude erring channel
									erringChannel := fmt.Sprintf("%s/%d",
										d["erring_channel"], d["erring_direction"])
									exclude = append(exclude, erringChannel)
								} else {
									p.Logf("probe payment failed: %s", err.Error())
									return
								}
							}

							// payment went through
							break
						}

						// add channel to database
						var jchanneldata []byte
						db.Update(func(tx *bbolt.Tx) error {
							bucket := tx.Bucket([]byte("channels"))

							last, _ := bucket.Cursor().Last()
							jchanneldata, _ = json.Marshal(channel)
							bucket.Put(last, jchanneldata)
							return nil
						})

						// notify all peers of this new channel
						res, err = p.Client.Call("listpeers")
						if err != nil {
							p.Logf("failed to listpeers to notify: %s", err.Error())
							return
						}
						for _, peerdata := range res.Get("peers").Array() {
							peerid := peerdata.Get("id").String()
							p.Client.Call("dev-sendcustommsg", peerid, strconv.FormatInt(REPLY_CHANNEL, 16)+hex.EncodeToString(jchanneldata))
						}

					case MSG_REPORT_CHANNEL:
					case MSG_REQUEST_CHANNEL:
						channelid, err := hex.DecodeString(message[4:])
						if err != nil {
							p.Logf("invalid channel-request message: %s", err.Error())
							return
						}

						db.View(func(tx *bbolt.Tx) error {
							bucket := tx.Bucket([]byte("channels"))
							channeldata := bucket.Get(channelid)
							if channeldata == nil {
								p.Logf("requested channel %s not found", channelid)
								return nil
							}
							_, err := p.Client.Call("dev-sendcustommsg", peer, strconv.FormatInt(REPLY_CHANNEL, 16)+hex.EncodeToString(channeldata))
							if err != nil {
								p.Logf("error sending reply: %s", err.Error())
							}
							return nil
						})
					case REPLY_CHANNEL:
						// we got a channel
						// if is from a server we trust, add it to our database

					case REPLY_INVOICE:
						if channel, ok := channelswaitingtosend[peer]; ok {
							// if this is expected, pay the invoice and send the channel
							jchanneldata, _ := json.Marshal(channel)
							_, err := p.Client.Call("dev-sendcustommsg", peer, strconv.FormatInt(MSG_ADD_CHANNEL, 16)+hex.EncodeToString(jchanneldata))
							if err != nil {
								p.Logf("error sending channel-add: %s", err.Error())
							}
						}
					}

					return
				},
			},
		},
		OnInit: func(p *plugin.Plugin) {
			// open database
			dbfile := filepath.Join(filepath.Dir(p.Client.Path), DATABASE_FILE)
			db, err = bbolt.Open(dbfile, 0644, nil)
			if err != nil {
				p.Logf("unable to open database at %s: %w", dbfile, err)
				os.Exit(1)
			}
			defer db.Close()

			// create channel bucket
			db.Update(func(tx *bbolt.Tx) error {
				tx.CreateBucketIfNotExists([]byte("channels"))
				return nil
			})

			// get new channels from servers from time to time
			serverlist := p.Args.Get("chanstore-connect").String()
			for _, server := range strings.Split(serverlist, ",") {
				if server == "" {
					continue
				}
				go getUpdates(p, server)
			}
		},
	}

	p.Run()
}

func getUpdates(p *plugin.Plugin, server string) {
	// on startup we query the server for updates since our last
	// we expect it to notify us if new channels appear
	for {
		db.Update(func(tx *bbolt.Tx) error {
			bucket := tx.Bucket([]byte("channels"))

			last, _ := bucket.Cursor().Last()
			p.Logf("querying since %d", last)

			// for _, channels := range res {
			// 	next, _ := bucket.NextSequence()
			// 	p.Logf("saving channel %v at sequence %d", channel, next)
			// }

			return nil
		})

		time.Sleep(15 * time.Minute)
	}
}
