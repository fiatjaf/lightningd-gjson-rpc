package server

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
	"github.com/tidwall/buntdb"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func GotPayment(p *plugin.Plugin, params plugin.Params) {
	label := params["invoice_payment"].(map[string]interface{})["label"].(string)
	if strings.HasPrefix(label, "lnurl/") {
		p.Logf("got payment %s", label)
		id := strings.Split(label, "/")[1] // labels are "lnurl/:id"
		var invoice string

		err := db.Update(func(tx *buntdb.Tx) error {
			var templateId string
			tx.AscendEqual("invoices", fmt.Sprintf(`{"id": "%s"}`, id), func(key, value string) bool {
				invoice = value

				// get template id
				templateId = gjson.Get(value, "template").String()

				return false
			})

			// update invoice status
			invoice, _ = sjson.Set(invoice, "paid", true)
			invoice, _ = sjson.Set(invoice, "paid_at", time.Now())
			_, _, err = tx.Set("template/"+templateId+"/invoice/"+id, invoice, nil /* clear up TTL */)
			if err != nil {
				return err
			}

			// get webhook URL from template
			template, err := tx.Get("template/" + templateId)
			if err != nil {
				return err
			}

			// dispatch webhook
			wh := gjson.Parse(template).Get("webhook")
			if wh.Exists() {
				whurl := wh.String()
				_, err := http.Post(whurl, "application/json",
					bytes.NewBufferString(invoice))
				if err != nil {
					p.Logf("error dispatching webhook to %s: %s",
						whurl, err.Error())
				} else {
					p.Logf("webhook dispatched to %s", whurl)
				}
			}

			return nil
		})
		if err != nil {
			p.Logf("error handling payment: %s", err.Error())
		}
	}
}
