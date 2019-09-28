package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
)

func main() {
	p := plugin.Plugin{
		Name: "webhook",
		Options: []plugin.Option{
			{
				Name:        "webhook",
				Type:        "string",
				Description: "The URL to which we will send notifications. Multiple URLs can be passed separated by commas. To filter notification by either {invoice|payment|forward}, pass the URL with a query string like https://url.com/?filter-event=payment",
			},
		},
		Subscriptions: []plugin.Subscription{
			{
				"invoice_payment",
				func(p *plugin.Plugin, params plugin.Params) {
					payload := params["invoice_payment"]
					for _, url := range getURLs(p.Args["webhook"].(string), "invoice") {
						dispatchWebhook(p, url, payload)
					}
				},
			},
			{
				"sendpay_success",
				func(p *plugin.Plugin, params plugin.Params) {
					payload := params["sendpay_success"]
					for _, url := range getURLs(p.Args["webhook"].(string), "payment") {
						dispatchWebhook(p, url, payload)
					}
				},
			},
			{
				"forward_event",
				func(p *plugin.Plugin, params plugin.Params) {
					payload := params["forward_event"]
					for _, url := range getURLs(p.Args["webhook"].(string), "forward") {
						dispatchWebhook(p, url, payload)
					}
				},
			},
		},
		Dynamic: true,
	}

	p.Run()
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
		} else {
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
