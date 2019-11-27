package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

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

func main() {
	http.DefaultTransport.(*http.Transport).Proxy = http.ProxyFromEnvironment

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
				htlc_accepted,
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
