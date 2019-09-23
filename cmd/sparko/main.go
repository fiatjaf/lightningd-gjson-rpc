package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"

	"github.com/NYTimes/gziphandler"
	"github.com/elazarl/go-bindata-assetfs"
	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
	"github.com/gorilla/mux"
	"github.com/gorilla/securecookie"
)

var err error
var scookie = securecookie.New(securecookie.GenerateRandomKey(32), nil)
var accessKey string
var manifestKey string
var login string
var ee chan event
var keys Keys

var httpPublic = &assetfs.AssetFS{Asset: Asset, AssetDir: AssetDir, Prefix: ""}

func main() {
	p := plugin.Plugin{
		Name: "sparko",
		Options: []plugin.Option{
			{"sparko-host", "string", "127.0.0.1", "http(s) server listen address"},
			{"sparko-port", "string", "9737", "http(s) server port"},
			{"sparko-login", "string", nil, "http basic auth login, \"username:password\" format"},
			{"sparko-tls-path", "string", nil, "directory to read/store key.pem and cert.pem for TLS"},
			{"sparko-keys", "string", "", "semicolon-separated list of key-permissions pairs"},
		},
		Subscriptions: []plugin.Subscription{
			{
				"invoice_payment",
				func(p *plugin.Plugin, params plugin.Params) {
					label := params["invoice_payment"].(map[string]interface{})["label"].(string)
					inv, err := p.Client.Call("waitinvoice", label)
					if err != nil {
						p.Log("Failed to get invoice on invoice_payment notification: " + err.Error())
						return
					}
					ee <- event{typ: "inv-paid", data: inv.String()}
				},
			},
			{
				"sendpay_success",
				func(p *plugin.Plugin, params plugin.Params) {
					payload := params["sendpay_success"]
					jpay, err := json.Marshal(payload)
					if err != nil {
						p.Log("Failed to encode payment success info: " + err.Error())
						return
					}
					ee <- event{typ: "pay-sent", data: string(jpay)}
				},
			},
		},
		OnInit: func(p *plugin.Plugin) {
			// compute access key
			login, _ = p.Args.String("sparko-login")
			if login != "" {
				accessKey = hmacStr(login, "access-key")
				manifestKey = hmacStr(accessKey, "manifest-key")
			}

			// permissions
			if keypermissions, err := p.Args.String("sparko-keys"); err == nil {
				keys, err = readPermissionsConfig(keypermissions)
				if err != nil {
					p.Log("Error reading permissions config: " + err.Error())
					return
				}
			}

			// start eventsource thing
			es := startStreams(p)

			// declare routes
			router := mux.NewRouter()
			router.Use(authMiddleware)
			router.Path("/stream").Methods("GET").Handler(es)
			router.Path("/rpc").Methods("POST").Handler(
				gziphandler.GzipHandler(
					http.HandlerFunc(handleRPC),
				),
			)

			if login != "" {
				// web ui
				router.Path("/").Methods("GET").HandlerFunc(
					func(w http.ResponseWriter, r *http.Request) {
						indexb, err := Asset("index.html")
						if err != nil {
							w.WriteHeader(404)
							return
						}
						indexb = bytes.Replace(indexb, []byte("{{accessKey}}"), []byte(accessKey), -1)
						indexb = bytes.Replace(indexb, []byte("{{manifestKey}}"), []byte(manifestKey), -1)
						w.Header().Set("Content-Type", "text/html")
						w.Write(indexb)
						return
					})
				router.PathPrefix("/").Methods("GET").Handler(http.FileServer(httpPublic))
			}

			// start server
			host, _ := p.Args.String("sparko-host")
			port, _ := p.Args.String("sparko-port")
			srv := &http.Server{
				Handler: router,
				Addr:    host + ":" + port,
				BaseContext: func(_ net.Listener) context.Context {
					return context.WithValue(
						context.Background(),
						"client", p.Client,
					)
				},
			}

			var listenerr error
			if tlspath, err := p.Args.String("sparko-tls-path"); err == nil {
				// expand tlspath from lightning dir
				if !filepath.IsAbs(tlspath) {
					tlspath = filepath.Join(filepath.Dir(p.Client.Path), tlspath)
				}

				if exists, err := pathExists(tlspath); err != nil {
					p.Log("tlspath ", tlspath, " couldn't be read: ", err)
					os.Exit(-1)
					return
				} else if !exists {
					// dir doesn't exist, create.
					err := os.MkdirAll(tlspath, os.ModeDir)
					if err != nil {
						p.Log("failed to make dir ", tlspath, ": ", err)
						os.Exit(-1)
						return
					}
				}
				// now dir exists, check cert.
				if exists, err := pathExists(filepath.Join(tlspath, "cert.pem")); err != nil {
					p.Log("certs at ", tlspath, " couldn't be read: ", err)
					os.Exit(-1)
					return
				} else if !exists {
					// don't have certs. generate.
					err := generateCert(tlspath)
					if err != nil {
						p.Log("failed to generate certificate at ", tlspath, ": ", err)
						os.Exit(-1)
						return
					}
				}

				// now we have certs!
				p.Log("HTTPS server on https://" + srv.Addr + "/")
				listenerr = srv.ListenAndServeTLS(path.Join(tlspath, "cert.pem"), path.Join(tlspath, "key.pem"))
			} else {
				p.Log("HTTP server on http://" + srv.Addr + "/")
				listenerr = srv.ListenAndServe()
			}

			p.Log("error listening: ", listenerr)
		},
		Dynamic: true,
	}

	p.Run()
}
