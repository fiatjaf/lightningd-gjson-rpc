package server

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
	"github.com/guiguan/caster"
	"github.com/tidwall/buntdb"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var payment = caster.New(context.TODO())

func GotPayment(p *plugin.Plugin, params plugin.Params) {
	label := params["invoice_payment"].(map[string]interface{})["label"].(string)
	if strings.HasPrefix(label, "lnurl/") {
		p.Logf("got payment %s", label)
		payment.TryPub(label)
	}
}

func sendWebhooks() {
	pays, _ := payment.Sub(context.TODO(), 1)
	defer payment.Unsub(pays)

	for {
		select {
		case ilabel := <-pays:
			label := ilabel.(string)
			id := strings.Split(label, "/")[1] // labels are "lnurl/:id"
			var invoice string

			db.Update(func(tx *buntdb.Tx) error {
				var templateId string
				tx.AscendEqual("invoices", id, func(key, value string) bool {
					invoice = value

					// update status
					invoice, _ = sjson.Set(invoice, "paid", true)
					invoice, _ = sjson.Set(invoice, "paid_at", time.Now())
					tx.Set(key, invoice, nil /* clear up TTL */)

					// get template id
					templateId = gjson.Get(value, "template").String()

					return false
				})

				// get webhook URL from template
				template, err := tx.Get("template/" + templateId)
				if err != nil {
					return err
				}

				// dispatch webhook
				wh := gjson.Parse(template).Get("webhook")
				if wh.Exists() {
					_, err := http.Post(wh.String(), "application/json",
						bytes.NewBufferString(invoice))
					if err != nil {
						log.Printf("error dispatching webhook to %s: %s",
							wh.String(), err.Error())
					}
				}

				return nil
			})
		}
	}
}
