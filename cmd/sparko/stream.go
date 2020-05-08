package main

import (
	"io/ioutil"
	"net/http"
	"strconv"
	"time"

	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
	"github.com/tidwall/gjson"
	"gopkg.in/antage/eventsource.v1"
)

type event struct {
	typ  string
	data string
}

func checkStreamPermission(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if permissions, ok := r.Context().Value("permissions").(map[string]bool); ok {
			if len(permissions) > 0 {
				if _, allowed := permissions["stream"]; !allowed {
					w.WriteHeader(401)
					return
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}

func startStreams(p *plugin.Plugin) eventsource.EventSource {
	id := 1

	es := eventsource.New(
		&eventsource.Settings{
			Timeout:        5 * time.Second,
			CloseOnTimeout: true,
			IdleTimeout:    300 * time.Minute,
		},
		func(req *http.Request) [][]byte {
			return [][]byte{
				[]byte("X-Accel-Buffering: no"),
				[]byte("Cache-Control: no-cache"),
				[]byte("Content-Type: text/event-stream"),
				[]byte("Connection: keep-alive"),
				[]byte("Access-Control-Allow-Origin: *"),
			}
		},
	)

	ee = make(chan event)
	go pollRate(p, ee)

	go func() {
		time.Sleep(1 * time.Second)
		es.SendRetryMessage(3 * time.Second)
	}()

	go func() {
		for {
			time.Sleep(25 * time.Second)
			es.SendEventMessage("", "keepalive", "")
		}
	}()

	go func() {
		for {
			select {
			case e := <-ee:
				es.SendEventMessage(e.data, e.typ, strconv.Itoa(id))
			}
			id++
		}
	}()

	return es
}

func pollRate(p *plugin.Plugin, ee chan<- event) {
	time.Sleep(time.Minute * 1)

	defer pollRate(p, ee)

	resp, err := http.Get("https://www.bitstamp.net/api/v2/ticker/btcusd")
	if err != nil || resp.StatusCode >= 300 {
		p.Log(resp.StatusCode, " error fetching BTC price: ", err)
		return
	}
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		p.Log("Error decoding BTC price: ", err)
		return
	}

	lastRate := gjson.GetBytes(b, "last").String()
	ee <- event{typ: "btcusd", data: `"` + lastRate + `"`}

	time.Sleep(time.Minute * 4)
}
