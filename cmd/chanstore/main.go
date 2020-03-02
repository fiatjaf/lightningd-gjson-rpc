package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/coreos/bbolt"
	"github.com/cretz/bine/tor"
	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
	"github.com/gorilla/mux"
)

const (
	DATABASE_FILE = "chanstore.db"
)

var db *bbolt.DB
var err error

func main() {
	p := plugin.Plugin{
		Name:    "chanstore",
		Version: "v0.1",
		Options: []plugin.Option{
			{
				"chanstore-server",
				"bool",
				false,
				"Should run a chanstore hidden service or not.",
			},
		},
		Subscriptions: []plugin.Subscription{
			{
				"invoice_payment",
				func(p *plugin.Plugin, params plugin.Params) {
					label := params.Get("label").String()
					preimage := params.Get("preimage").String()

				},
			},
		},
		Hooks: []plugin.Hook{
			{
				"rpc_command",
				func(p *plugin.Plugin, params plugin.Params) (resp interface{}) {
					if params.Get("method").String() != "getroute" {
						return map[string]string{"result": "continue"}
					}

					toExclude := params.Get("params.exclude").Array()
					exclude := make([]string, len(toExclude))
					for i, scid := range toExclude {
						exclude[i] = scid.String()
					}

					fuzz := params.Get("params.fuzzpercent").String()
					fuzzpercent, err := strconv.ParseFloat(fuzz, 64)
					if err != nil {
						fuzzpercent = 5.0
					}

					route, err := p.Client.GetRoute(
						params.Get("params.id").String(),
						params.Get("params.msatoshi").Int(),
						int(params.Get("params.riskfactor").Int()),
						params.Get("params.cltv").Int(),
						params.Get("params.fromid").String(),
						fuzzpercent,
						exclude,
						int(params.Get("params.maxhops").Int()),
					)

					if err != nil {
						p.Logf("failed to getroute: %w, falling back to default.", err)
						return map[string]string{"result": "continue"}
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

			// now we determine if we are going to be a channel server or not
			if !p.Args.Get("chanstore-server").Bool() {
				return
			}

			// create allowances and events bucket
			db.Update(func(tx *bbolt.Tx) error {
				tx.CreateBucketIfNotExists([]byte("events"))
				tx.CreateBucketIfNotExists([]byte("allowances"))
				return nil
			})

			// define routes
			router := mux.NewRouter()
			router.Path("/add").HandlerFunc(add)
			router.Path("/events").HandlerFunc(get)

			// start tor with default config
			p.Log("Starting and registering onion service, please wait a couple of minutes...")
			t, err := tor.Start(nil, nil)
			if err != nil {
				p.Logf("Unable to start Tor: %w", err)
				return
			}
			defer t.Close()

			// wait at most a few minutes to publish the service
			listenCtx, listenCancel := context.WithTimeout(
				context.Background(), 3*time.Minute)
			defer listenCancel()

			// create a v3 onion service to listen on any port but show as 80
			onion, err := t.Listen(listenCtx, &tor.ListenConf{
				Version3:    true,
				RemotePorts: []int{80},
			})
			if err != nil {
				p.Logf("Unable to create onion service: %w", err)
				return
			}
			defer onion.Close()

			p.Logf("listening at http://%v.onion/", onion.ID)
			http.Serve(onion, router)
		},
	}

	p.Run()
}
