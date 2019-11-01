package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
	"github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/guiguan/caster"
	"github.com/tidwall/buntdb"
	"github.com/tidwall/gjson"
)

var err error
var db *buntdb.DB
var bot *tgbotapi.BotAPI
var awaken = caster.New(context.TODO()) // emits telegramIds
var paid = caster.New(context.TODO())   // emits preimages
var failed = caster.New(context.TODO()) // emits hashes
var info gjson.Result                   // the result from 'getinfo'

var continueHTLC = map[string]interface{}{"result": "continue"}
var failHTLC = map[string]interface{}{"result": "fail", "failure_code": 8194}

func main() {
	p := plugin.Plugin{
		Name:    "notifierbot",
		Version: "v0.1",
		Options: []plugin.Option{
			{"notifierbot-token", "string", nil, "Telegram bot token"},
			{"notifierbot-dbfile", "string", "notifierbot/data.db",
				"Where we'll store data, path can be relative to lightning-dir"},
		},
		Subscriptions: []plugin.Subscription{
			{
				"connect",
				func(p *plugin.Plugin, params plugin.Params) {
					nodeid := params["id"].(string)
					if bot == nil {
						return
					}

					db.View(func(tx *buntdb.Tx) error {
						val, err := tx.Get(nodeid)
						if err != nil {
							return err
						}

						telegramId := gjson.Parse(val).Get("telegram").Int()
						awaken.TryPub(telegramId)
						return nil
					})
				},
			},
			{
				"sendpay_success",
				func(p *plugin.Plugin, params plugin.Params) {
					preimage := params["sendpay_success"].(map[string]interface{})["payment_preimage"].(string)
					paid.TryPub(preimage)
				},
			},
		},
		Hooks: []plugin.Hook{
			{
				"htlc_accepted",
				func(p *plugin.Plugin, params plugin.Params) (resp interface{}) {
					scid, msatoshi, ok := getOnionData(params["onion"])
					if !ok {
						return continueHTLC
					}
					htlc := params["htlc"].(map[string]interface{})
					hash := htlc["payment_hash"].(string)
					timeLeft := int(htlc["cltv_expiry_relative"].(float64))
					p.Logf("htlc_accepted %dmsat next=%s hash=%s timeleft=%d", msatoshi, scid, hash, timeLeft)

					// data used to notify the user and then pay his original invoice
					var telegramId int64
					var originalbolt11 string
					var isPaid bool
					var isLocalPeer bool

					// is this an HTLC that's being retried? we should check if we've paid it to someone else
					// already so we can claim it and don't lose money
					sendpays, err := p.Client.CallNamed("listsendpays", "payment_hash", hash)
					if err != nil {
						p.Log("Unexpectedly failed to call listsendpays on htlc_accepted: " + err.Error())
						return continueHTLC
					}

					sendpay := sendpays.Get("payments.0")
					if sendpay.Exists() {
						// this payment was tried / it's been tried still
						switch sendpay.Get("status").String() {
						case "pending":
							// we'll jump to the listen flow, but skip paying and
							// notifying the user as that was already done
							isPaid = true
							goto listen
						case "failed":
							// even in "failed" case this might still be pending if it's recent enough
							if !time.Unix(sendpay.Get("created_at").Int(), 0).Before(
								time.Now().Add(time.Minute * 5),
							) {
								isPaid = true
								goto listen
							}
						case "complete":
							// no matter what happened, if we have a preimage for a pending HTLC we resolve it
							return map[string]interface{}{
								"result":      "resolve",
								"payment_key": sendpay.Get("payment_preimage").String(),
							}
						}
					}

					// now we start the normal flow
					waitForBot(bot)

					// get peer for this channel -- or none and just continue the HTLC flow
					if res, err := p.Client.Call("listpeers"); err != nil {
						p.Logf("failed to listpeers, something is badly wrong")
						return continueHTLC
					} else if peer := res.Get(`peers.#(channels.0.short_channel_id=="` + scid + `")`); peer.Exists() {
						// it's a peer with a direct channel here
						isLocalPeer = true

						// if it's already connected, just continue
						if peer.Get("connected").Bool() {
							return continueHTLC
						}

						// otherwise get telegram id to notify
						nodeid := peer.Get("id").String()
						db.View(func(tx *buntdb.Tx) error {
							val, err := tx.Get(nodeid)
							if err != nil {
								return err
							}

							telegramId = gjson.Parse(val).Get("telegram").Int()
							return nil
						})
					} else {
						// maybe it's a peer not directly connected. search our database of fake invoices.
						db.View(func(tx *buntdb.Tx) error {
							val, err := tx.Get(hash)
							if err != nil {
								return err
							}

							data := gjson.Parse(val)
							telegramId = data.Get("telegram").Int()
							originalbolt11 = data.Get("originalbolt11").String()
							return nil
						})
					}

					if telegramId == 0 {
						// didn't find a telegram peer
						p.Log("didn't find a telegram peer for HTLC, continuing")
						return continueHTLC
					}

					// send telegram message
					p.Logf("htlc_accepted for telegram user %d, hash %s, %dmsat", telegramId, hash, msatoshi)
					err = notify(telegramId, hash, msatoshi)
					if err != nil {
						// failed to notify, so fail
						return failHTLC
					}

				listen:
					// now we wait until peer is connected to release the HTLC or give up after 30 minutes
					wakes, _ := awaken.Sub(context.TODO(), 1)
					defer awaken.Unsub(wakes)
					pays, _ := paid.Sub(context.TODO(), 1)
					defer paid.Unsub(pays)
					fails, _ := failed.Sub(context.TODO(), 1)
					defer failed.Unsub(fails)

					sendingPayment := false
					for {
						select {
						case <-time.After(30 * time.Minute):
							if !sendingPayment {
								p.Logf("30min timeout for HTLC %s. failing.", hash)
								return failHTLC
							}
						case tgid := <-wakes:
							// peer is awaken, so release the payment or do the preimage-fetching gimmick
							if int64(tgid.(int)) == telegramId {
								p.Logf("peer %d is online. proceeding with HTLC %s.", telegramId, hash)
								// user is now online, we can proceed to send the payment
								if isLocalPeer {
									// invoice points to direct channel with peer, so just continue
									return continueHTLC
								} else if !isPaid {
									// peer is elsewhere on the network, so send him a payment
									if timeLeft < 32 {
										// too risky, but this should never happen and should always be 288 anyway
										return failHTLC
									}

									p.Logf("sending payment to %s to fetch preimage for %s, timeleft=%d",
										originalbolt11, hash, timeLeft)
									go func() {
										sendingPayment = true
										ok, _, tries, err := p.Client.PayAndWaitUntilResolution(
											originalbolt11,
											map[string]interface{}{
												"maxfeepercent": 0.3, // because we add 0.3%
												"maxdelaytotal": timeLeft - 23,
											})

										if err != nil {
											p.Logf("error sending preimage-fetching payment: %s", err)
											failed.TryPub(hash)
										}
										if ok == false {
											p.Logf("failed to send preimage-fetching payment: %v", tries)
											failed.TryPub(hash)
										}

										// we only handle failure here.
										// in case of success we should see a sendpay_success notification.
									}()
								}
							}
						case fhash := <-fails:
							// see if this hash matches ours and fail if yes
							if fhash.(string) == hash {
								p.Logf("%d will not be able to receive HTLC %s. failing,.", telegramId, hash)
								return failHTLC
							}
						case preimage := <-pays:
							// see if this preimage matches our hash and resolve if yes
							if bpreimage, err := hex.DecodeString(preimage.(string)); err == nil {
								if bhash := sha256.Sum256(bpreimage); hex.EncodeToString(bhash[:]) == hash {
									p.Logf("got preimage for HTLC %s from user %d. resolving.", hash, telegramId)
									return map[string]interface{}{
										"result":      "resolve",
										"payment_key": preimage,
									}
								}
							}
						}
					}

					return failHTLC
				},
			},
		},
		OnInit: func(p *plugin.Plugin) {
			// get our node info
			info, err = p.Client.Call("getinfo")
			if err != nil {
				p.Logf("failed to getinfo: " + err.Error())
				return
			}
			// get params
			botToken, _ := p.Args.String("notifierbot-token")
			databaseFile, _ := p.Args.String("notifierbot-dbfile")
			if !filepath.IsAbs(databaseFile) {
				// expand db path from lightning dir
				databaseFile = filepath.Join(filepath.Dir(p.Client.Path), databaseFile)
				// create dir if not exists
				os.MkdirAll(filepath.Dir(databaseFile), os.ModePerm)
			}

			// open database
			db, err = buntdb.Open(databaseFile)
			if err != nil {
				p.Logf("failed to open database at " + databaseFile + ": " + err.Error())
				return
			}
			defer db.Close()

			// create bot
			bot, err = tgbotapi.NewBotAPI(botToken)
			if err != nil {
				p.Logf("failed to instantiate bot: " + err.Error())
				return
			}

			// listen for bot updates
			var lastUpdate int
			db.View(func(tx *buntdb.Tx) error {
				val, err := tx.Get("last-update")
				if err != nil {
					return err
				}

				lastUpdate = int(gjson.Parse(val).Int())
				return nil
			})

			updates, err := bot.GetUpdatesChan(tgbotapi.UpdateConfig{
				Offset:  lastUpdate + 1,
				Limit:   100,
				Timeout: 120,
			})
			if err != nil {
				p.Log("couldn't start listening for Telegram events: " + err.Error())
				return
			}

			log.Print(updates)
			for update := range updates {
				handle(p, update)
				go db.Update(func(tx *buntdb.Tx) error {
					tx.Set("last-update", strconv.Itoa(update.UpdateID), nil)
					return nil
				})
			}
		},
	}

	p.Run()
}
