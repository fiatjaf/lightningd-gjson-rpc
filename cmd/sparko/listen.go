package main

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
	"golang.org/x/crypto/acme/autocert"
)

func listen(p *plugin.Plugin, router http.Handler) {
	host, _ := p.Args.String("sparko-host")
	port, _ := p.Args.String("sparko-port")
	letsemail, _ := p.Args.String("sparko-letsencrypt-email")
	tlspath := ""
	if giventlspath, err := p.Args.String("sparko-tls-path"); err == nil {
		if !filepath.IsAbs(giventlspath) {
			// expand tlspath from lightning dir
			tlspath = filepath.Join(filepath.Dir(p.Client.Path), giventlspath)
		} else {
			tlspath = giventlspath
		}
	}

	var listenerr error
	if letsemail != "" {
		if len(strings.Split(host, ".")) == 4 && len(host) <= 15 {
			p.Log("when using letsencrypt `sparko-host` must be a domain, not IP")
			return
		}
		if port != DEFAULTPORT {
			p.Log("when using letsencrypt will ignore `sparko-port` and bind to 80 and 443")
		}
		if tlspath == "" {
			p.Log("must specify a valid `sparko-tls-path` directory when using letsencrypt")
			return
		}

		if !pathExists(tlspath) {
			os.MkdirAll(tlspath, os.ModePerm)
		}

		certManager := autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(host),
			Cache:      autocert.DirCache(tlspath),
		}

		server := &http.Server{
			Addr: ":https",
			TLSConfig: &tls.Config{
				GetCertificate: certManager.GetCertificate,
			},
			Handler: router,
			BaseContext: func(_ net.Listener) context.Context {
				return context.WithValue(
					context.Background(),
					"client", p.Client,
				)
			},
			TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		}

		go http.ListenAndServe(":http", certManager.HTTPHandler(nil))
		listenerr = server.ListenAndServeTLS("", "")
	} else {
		srv := &http.Server{
			Addr:    host + ":" + port,
			Handler: router,
			BaseContext: func(_ net.Listener) context.Context {
				return context.WithValue(
					context.Background(),
					"client", p.Client,
				)
			},
			TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		}

		if tlspath != "" {
			if !pathExists(tlspath) || !pathExists(filepath.Join(tlspath, "cert.pem")) || !pathExists(filepath.Join(tlspath, "key.pem")) {
				p.Log("couldn't find certificates. to create, do `mkdir -p '" + tlspath + "' && cd '" + tlspath + "' && openssl genrsa -out key.pem 2048 && openssl req -new -x509 -sha256 -key key.pem -out cert.pem -days 3650`")
				return
			}

			p.Log("HTTPS server on https://" + srv.Addr + "/")
			listenerr = srv.ListenAndServeTLS(path.Join(tlspath, "cert.pem"), path.Join(tlspath, "key.pem"))
		} else {
			p.Log("HTTP server on http://" + srv.Addr + "/")
			listenerr = srv.ListenAndServe()
		}
	}

	p.Log("error listening: " + listenerr.Error())
}
