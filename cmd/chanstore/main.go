package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/bbolt"
	"github.com/cretz/bine/tor"
	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
	"github.com/gorilla/mux"
)

const (
	DATABASE_FILE = "chanstore.db"
)

var (
	continuehook = map[string]string{"result": "continue"}
	multipliers  = map[string]float64{
		"msat": 1,
		"sat":  1000,
		"btc":  100000000000,
	}
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
				"chanstore-server",
				"bool",
				false,
				"Should run a chanstore hidden service or not.",
			},
			{
				"chanstore-connect",
				"string",
				"",
				"chanstore services to connect to.",
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

			// now we determine if we are going to be a channel server or not
			if !p.Args.Get("chanstore-server").Bool() {
				// from now on only run server-specific code
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
			p.Log("starting onion service, please wait a couple of minutes...")
			t, err := tor.Start(nil, nil)
			if err != nil {
				p.Logf("unable to start Tor: %w", err)
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
				p.Logf("unable to create onion service: %w", err)
				return
			}
			defer onion.Close()

			p.Logf("listening at http://%v.onion/", onion.ID)
			http.Serve(onion, router)
		},
	}

	p.Run()
}
