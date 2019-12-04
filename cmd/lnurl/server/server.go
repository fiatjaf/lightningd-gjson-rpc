package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
	"github.com/gorilla/mux"
	"github.com/tidwall/buntdb"
	"golang.org/x/crypto/acme/autocert"
)

const DEFAULTPORT = "14421"

var db *buntdb.DB
var keys = make(map[string]bool)
var err error

func Start(p *plugin.Plugin) {
	if keylist, err := p.Args.String("lnurl-keys"); err == nil {
		for _, key := range strings.Split(keylist, ";") {
			key = strings.TrimSpace(key)
			if key != "" {
				keys[key] = true
			}
		}
	}
	if len(keys) == 0 {
		p.Log("no keys set. not starting lnurl server.")
		return
	} else {
		p.Log("Keys read: ", keys)
	}

	dbpath := getPath(p, "lnurl-db-path")
	os.MkdirAll(filepath.Dir(dbpath), os.ModePerm)
	db, err = buntdb.Open(dbpath)
	if err != nil {
		p.Log("failed to open db at " + dbpath + ". stopping here.")
		return
	}
	p.Log("opened database at " + dbpath)

	err = db.Shrink()
	if err != nil {
		p.Logf("error shrinking database: %s", err.Error())
	}

	db.CreateIndex("invoices", "template/*/invoice/*", buntdb.IndexJSON("id"))

	router := mux.NewRouter()
	router.Use(allJSONMiddleware)
	router.Use(authMiddleware)
	router.Path("/templates").Methods("GET").HandlerFunc(listTemplates)
	router.Path("/template/{id}").Methods("PUT").HandlerFunc(setTemplate)
	router.Path("/template/{id}").Methods("DELETE").HandlerFunc(deleteTemplate)
	router.Path("/template/{id}").Methods("GET").HandlerFunc(getTemplate)
	router.Path("/template/{id}/lnurl").Methods("GET").HandlerFunc(getLNURL)
	router.Path("/template/{id}/invoices").Methods("GET").HandlerFunc(listInvoices)
	router.Path("/invoice/{id}").Methods("GET").HandlerFunc(getInvoice)
	router.Path("/sse-stream").Methods("GET").HandlerFunc(payStreamSSE)
	router.Path("/ws-stream").Methods("GET").HandlerFunc(payStreamWS)
	router.PathPrefix("/lnurl/params/").Methods("GET").HandlerFunc(lnurlPayParams)
	router.PathPrefix("/lnurl/values/").Methods("GET").HandlerFunc(lnurlPayValues)

	listen(p, router)
}

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/lnurl/") {
			next.ServeHTTP(w, r)
			return
		}

		if _, key, ok := r.BasicAuth(); ok {
			if _, ok := keys[key]; ok {
				next.ServeHTTP(w, r)
				return
			}
		}

		w.WriteHeader(401)
		json.NewEncoder(w).Encode(ErrorResponse{"wrong API key"})
		return
	})
}

func allJSONMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
		return
	})
}

func listen(p *plugin.Plugin, router *mux.Router) {
	host, _ := p.Args.String("lnurl-host")
	port, _ := p.Args.String("lnurl-port")
	letsemail, _ := p.Args.String("lnurl-letsencrypt-email")
	tlspath := getPath(p, "lnurl-tls-path")
	domain, _ := p.Args.String("lnurl-domain")
	if domain == "" {
		domain = host + ":" + port
	}

	hmacKeyStr, _ := p.Args.String("lnurl-hmac-key")
	var hmacKey []byte
	if hmacKeyStr != "" {
		hmacKey = []byte(hmacKeyStr)
	} else {
		hmacKey = getDefaultHMACKey(p.Client)
	}

	var serviceURL string
	getBaseContext := func(_ net.Listener) context.Context {
		return context.WithValue(
			context.WithValue(
				context.WithValue(
					context.Background(),
					"hmacKey", hmacKey,
				),
				"plugin", p,
			),
			"serviceURL", serviceURL,
		)
	}

	var listenerr error
	if letsemail != "" {
		if domain == "" || (len(strings.Split(domain, ".")) == 4 && len(domain) <= 15) {
			p.Log("when using letsencrypt specify `lnurl-domain`")
			return
		}
		if port != DEFAULTPORT {
			p.Log("when using letsencrypt will ignore `lnurl-port` and bind to 80 and 443")
		}
		if tlspath == "" {
			p.Log("must specify a valid `lnurl-tls-path` directory when using letsencrypt")
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

		srv := &http.Server{
			Addr: ":https",
			TLSConfig: &tls.Config{
				GetCertificate: certManager.GetCertificate,
			},
			Handler:      router,
			BaseContext:  getBaseContext,
			TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		}

		p.Log("HTTPS server on https://" + srv.Addr + "/")
		serviceURL = "https://" + domain
		p.Logf("lnurls based on %s", serviceURL)
		go http.ListenAndServe(":http", certManager.HTTPHandler(nil))
		listenerr = srv.ListenAndServeTLS("", "")
	} else {
		srv := &http.Server{
			Addr:         host + ":" + port,
			Handler:      router,
			BaseContext:  getBaseContext,
			TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		}

		if tlspath != "" {
			if !pathExists(tlspath) || !pathExists(filepath.Join(tlspath, "cert.pem")) || !pathExists(filepath.Join(tlspath, "key.pem")) {
				p.Log("couldn't find certificates. to create, do `mkdir -p '" + tlspath + "' && cd '" + tlspath + "' && openssl genrsa -out key.pem 2048 && openssl req -new -x509 -sha256 -key key.pem -out cert.pem -days 3650`")
				return
			}

			p.Log("HTTPS server on https://" + srv.Addr + "/")
			serviceURL = "https://" + domain
			p.Logf("lnurls based on %s", serviceURL)
			listenerr = srv.ListenAndServeTLS(path.Join(tlspath, "cert.pem"), path.Join(tlspath, "key.pem"))
		} else {
			p.Log("HTTP server on http://" + srv.Addr + "/")
			serviceURL = "https://" + domain
			p.Logf("lnurls based on %s", serviceURL)
			listenerr = srv.ListenAndServe()
		}
	}

	p.Log("error listening: " + listenerr.Error())
}
