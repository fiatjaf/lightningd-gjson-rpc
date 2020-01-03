package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
)

func main() {
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	http.DefaultTransport.(*http.Transport).Proxy = http.ProxyFromEnvironment

	p := plugin.Plugin{
		Name:    "webhook",
		Version: "v3.1",
		Options: []plugin.Option{
			{
				Name:        "webhook",
				Type:        "string",
				Description: "The URL to which we will send notifications. Multiple URLs can be passed separated by commas. To filter notification by either {invoice|payment|forward}, pass the URL with a query string like https://url.com/?filter-event=payment",
			},
		},
		Subscriptions: []plugin.Subscription{
			subscription("channel_opened"),
			subscription("connect"),
			subscription("disconnect"),
			subscription("invoice_payment"),
			subscription("warning"),
			subscription("forward_event"),
			subscription("sendpay_success"),
			subscription("sendpay_failure"),
		},
		Dynamic: true,
	}

	p.Run()
}

func subscription(kind string) plugin.Subscription {
	return plugin.Subscription{
		kind,
		func(p *plugin.Plugin, params plugin.Params) {
			var payload interface{}
			payload = params

			urldata, _ := p.Args.String("webhook")
			if urldata != "" {
				for _, url := range getURLs(urldata, kind) {
					dispatchWebhook(p, url, payload)
				}
			}
		},
	}
}

func getURLs(optvalue string, kind string) (urls []string) {
	for _, entry := range strings.Split(optvalue, ",") {
		entry := strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		u, err := url.Parse(entry)
		if err != nil {
			continue
		}

		if evs, ok := u.Query()["filter-event"]; ok {
			for _, ev := range evs {
				if ev == kind {
					urls = append(urls, entry)
				}
			}
		} else if kind == "sendpay_success" || kind == "invoice_payment" {
			urls = append(urls, entry)
		}
	}
	return
}

func dispatchWebhook(p *plugin.Plugin, url string, payload interface{}) {
	j, err := json.Marshal(payload)
	if err != nil {
		p.Log("failed to encode json payload: " + err.Error())
		return
	}

	req, err := http.Post(url, "application/json", bytes.NewBuffer(j))
	if err != nil {
		p.Log("failed to send webhook to " + url + ": " + err.Error())
		return
	}
	if req.StatusCode >= 300 {
		b, _ := ioutil.ReadAll(req.Body)
		p.Log(
			"webhook handler at " + url +
				" returned error (" + strconv.Itoa(req.StatusCode) + "): " + string(b),
		)
		return
	}
}
