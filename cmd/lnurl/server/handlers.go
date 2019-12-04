package server

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/fiatjaf/go-lnurl"
	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
	"github.com/gorilla/mux"
	"github.com/tidwall/buntdb"
)

func listTemplates(w http.ResponseWriter, r *http.Request) {
	db.View(func(tx *buntdb.Tx) error {
		list := make([]json.RawMessage, 0)
		tx.AscendRange("", "template/", "template~", func(key, value string) bool {
			list = append(list, json.RawMessage(value))
			return true
		})
		json.NewEncoder(w).Encode(list)
		return nil
	})
}

func setTemplate(w http.ResponseWriter, r *http.Request) {
	db.Update(func(tx *buntdb.Tx) error {
		id := mux.Vars(r)["id"]
		// if _, err := tx.Get("template/" + id); err != buntdb.ErrNotFound {
		// 	json.NewEncoder(w).Encode(ErrorResponse{
		// 		"can't change existing templates because that breaks metadata!"})
		// 	return nil
		// }

		var t Template
		err := json.NewDecoder(r.Body).Decode(&t)
		if err != nil {
			json.NewEncoder(w).Encode(ErrorResponse{err.Error()})
			return nil
		}
		t.Id = id

		j, _ := json.Marshal(t)
		tx.Set("template/"+id, string(j), nil)
		p := r.Context().Value("plugin").(*plugin.Plugin)
		p.Logf("saving template %s", id)

		json.NewEncoder(w).Encode(t)
		return nil
	})
}

func deleteTemplate(w http.ResponseWriter, r *http.Request) {
	db.Update(func(tx *buntdb.Tx) error {
		id := mux.Vars(r)["id"]
		tx.Delete("template/" + id)
		p := r.Context().Value("plugin").(*plugin.Plugin)
		p.Logf("deleting template %s", id)
		json.NewEncoder(w).Encode(true)
		return nil
	})
}

func getTemplate(w http.ResponseWriter, r *http.Request) {
	db.View(func(tx *buntdb.Tx) error {
		id := mux.Vars(r)["id"]
		val, err := tx.Get("template/" + id)
		if err != nil {
			json.NewEncoder(w).Encode(ErrorResponse{id + " template not found"})
			return nil
		}

		json.NewEncoder(w).Encode(json.RawMessage(val))
		return nil
	})
}

func getLNURL(w http.ResponseWriter, r *http.Request) {
	db.View(func(tx *buntdb.Tx) error {
		id := mux.Vars(r)["id"]
		val, err := tx.Get("template/" + id)
		if err != nil {
			json.NewEncoder(w).Encode(ErrorResponse{id + " template not found"})
			return nil
		}

		var t Template
		err = json.Unmarshal([]byte(val), &t)
		if err != nil {
			json.NewEncoder(w).Encode(ErrorResponse{"failed to decode template: " + err.Error()})
			return nil
		}

		params := make(map[string]string)
		for k, v := range r.URL.Query() {
			params[k] = v[0]
		}

		url := t.MakeURL(
			r.Context().Value("serviceURL").(string)+"/lnurl/params",
			r.Context().Value("hmacKey").([]byte),
			params,
		)
		lnurlEncoded, err := lnurl.LNURLEncode(url)
		if err != nil {
			json.NewEncoder(w).Encode(ErrorResponse{"failed to encode lnurl: " + err.Error()})
			return err
		}
		fmt.Fprintln(w, lnurlEncoded)

		return nil
	})
}

func listInvoices(w http.ResponseWriter, r *http.Request) {
	db.View(func(tx *buntdb.Tx) error {
		id := mux.Vars(r)["id"]
		var list []json.RawMessage
		tx.AscendRange("",
			"template/"+id+"/invoice/",
			"template/"+id+"/invoice~",
			func(key, value string) bool {
				list = append(list, json.RawMessage(value))
				return true
			})
		json.NewEncoder(w).Encode(list)
		return nil
	})
}

func getInvoice(w http.ResponseWriter, r *http.Request) {
	db.View(func(tx *buntdb.Tx) error {
		invoiceId := mux.Vars(r)["id"]
		tx.AscendEqual("invoices", invoiceId, func(key, value string) bool {
			json.NewEncoder(w).Encode(json.RawMessage(value))
			return true
		})
		return nil
	})
}

func payStreamSSE(w http.ResponseWriter, r *http.Request) {
	return
}

func payStreamWS(w http.ResponseWriter, r *http.Request) {
	return
}

func lnurlPayParams(w http.ResponseWriter, r *http.Request) {
	t, params, err := FromURL(r.URL, r.Context().Value("hmacKey").([]byte))
	if err != nil {
		json.NewEncoder(w).Encode(lnurl.ErrorResponse("failed to parse URL: " + err.Error()))
		return
	}

	price, err := t.GetInvoicePrice(params)
	if err != nil {
		json.NewEncoder(w).Encode(lnurl.ErrorResponse("failed to calculate price: " + err.Error()))
		return
	}

	p := r.Context().Value("plugin").(*plugin.Plugin)
	p.Logf("responding lnurl-pay 1st call on template %s, price: %d", t.Id, price)

	serviceURL := r.Context().Value("serviceURL").(string)
	json.NewEncoder(w).Encode(lnurl.LNURLPayResponse1{
		Tag: "payRequest",
		Callback: t.MakeURL(
			serviceURL+"/lnurl/values",
			r.Context().Value("hmacKey").([]byte),
			params,
		),
		EncodedMetadata: t.EncodedMetadata(params),
		MinSendable:     price,
		MaxSendable:     price,
	})
}

func lnurlPayValues(w http.ResponseWriter, r *http.Request) {
	t, params, err := FromURL(r.URL, r.Context().Value("hmacKey").([]byte))
	if err != nil {
		json.NewEncoder(w).Encode(lnurl.ErrorResponse("failed to parse URL: " + err.Error()))
		return
	}

	p := r.Context().Value("plugin").(*plugin.Plugin)
	invoice, err := t.GetInvoice(p, params)
	if err != nil {
		json.NewEncoder(w).Encode(lnurl.ErrorResponse("failed to generate invoice: " + err.Error()))
		return
	}

	// if a secret_code_key is in this template use it to create an hmac-code
	// code is a small confirmation code intented only for face-to-face interactions
	var sa *lnurl.SuccessAction
	if t.SecretCodeKey != "" {
		code := generateCode(&t, invoice)
		preimage, _ := hex.DecodeString(invoice.Preimage)
		sa, _ = lnurl.AESAction("CODE", preimage, code)
	}

	p.Logf("responding lnurl-pay 2nd call on template %s", t.Id)

	r.Header.Set("X-Invoice-Id", invoice.Id)
	json.NewEncoder(w).Encode(lnurl.LNURLPayResponse2{
		Routes:        make([][]lnurl.RouteInfo, 0),
		PR:            invoice.Bolt11,
		SuccessAction: sa,
	})
}
