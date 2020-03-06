package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/coreos/bbolt"
	lightning "github.com/fiatjaf/lightningd-gjson-rpc"
	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
)

const (
	DATABASE_FILE = "chanstore.db"

	REPLY_INVOICE        = 63241
	REPLY_CHANNEL        = 63243
	MSG_REQUEST_INVOICE  = 63245
	MSG_ADD_CHANNEL      = 63247
	MSG_REPORT_CHANNEL   = 63249
	MSG_REQUEST_CHANNELS = 63251

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
	serverlist            = make(map[string]bool)
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
				"Chanstore service addresses to fetch channels from, comma-separated.",
			},
			{
				"chanstore-server",
				"bool",
				false,
				"If enabled, run a chanstore server.",
			},
			{
				"chanstore-price",
				"integer",
				72,
				"Satoshi price to ask for peers to include a channel.",
			},
		},
		Hooks: []plugin.Hook{
			{
				"rpc_command",
				func(p *plugin.Plugin, payload plugin.Params) (resp interface{}) {
					rpc_command := payload.Get("rpc_command.rpc_command")

					switch rpc_command.Get("method").String() {
					case "getroute":
						return getroute(p, rpc_command)
					case "listchannels":
						return listchannels(p, rpc_command)
					default:
						return continuehook
					}
				},
			},
			{
				"custommsg",
				custommsg,
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

			// parse list of servers to connect to
			for _, server := range strings.Split(p.Args.Get("chanstore-connect").String(), ",") {
				if server == "" {
					continue
				}
				serverlist[server] = true
			}

			// create local channel bucket
			db.Update(func(tx *bbolt.Tx) error {
				// a bucket for channels people may send to us
				tx.CreateBucketIfNotExists([]byte("server"))

				// then there are other buckets, one with each peer id as name
				// for channels we may get from other servers and that we will
				// use locally
				for server, _ := range serverlist {
					tx.CreateBucketIfNotExists([]byte(server))
				}

				return nil
			})

			// connect and fetch channels from servers
			for server, _ := range serverlist {
				db.View(func(tx *bbolt.Tx) error {
					bucket := tx.Bucket([]byte(server))
					stats := bucket.Stats()

					// count the number of objects in a bucket
					// so we can tell the server and expect all the new stuff
					last := int64(stats.KeyN - 1)
					p.Logf("querying %s since %d", server, last)

					// we send this and then expect the server to send
					// all available channels to us
					_, err = p.Client.Call("dev-sendcustommsg", server,
						strconv.FormatInt(MSG_REQUEST_CHANNELS, 16)+
							strconv.FormatInt(last, 16))
					if err != nil {
						p.Logf("error sending channels-request: %s", err.Error())
						return nil
					}

					return nil
				})
			}
		},
	}

	p.Run()
}
