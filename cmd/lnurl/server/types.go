package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/fiatjaf/lightningd-gjson-rpc"
	"github.com/hoisie/mustache"
	"github.com/lucsky/cuid"
	"github.com/soudy/mathcat"
	"github.com/tidwall/buntdb"
)

type Template struct {
	Id           string            `json:"id"`
	QueryParams  map[string]bool   `json:"query_params,omitempty"`
	URLParams    []string          `json:"url_params,omitempty"`
	Metadata     map[string]string `json:"metadata"`
	PriceSatoshi interface{}       `json:"price_satoshi,omitempty"`
	Webhook      string            `json:"webhook"`
}

func (t *Template) MakeURL(baseURL string, params map[string]string) string {
	path := make([]string, len(t.URLParams))
	for i, key := range t.URLParams {
		value, _ := params[key]
		path[i] = fmt.Sprint(value)
	}

	var qs []string
	var qsencoded string
	for key, _ := range t.QueryParams {
		if value, ok := params[key]; ok {
			qs = append(qs,
				fmt.Sprintf("%s=%s", url.QueryEscape(key), url.QueryEscape(value)))
		}
	}
	if len(qs) > 0 {
		qsencoded = "?" + strings.Join(qs, "&")
	}

	return baseURL + "/" + t.Id + "/" + strings.Join(path, "/") + qsencoded
}

func FromURL(u *url.URL) (t Template, params map[string]string, err error) {
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}

	spl := strings.Split(u.Path, "/")
	if len(spl) < 4 {
		err = fmt.Errorf("invalid path: %s", u.Path)
		return
	}

	// get template from URL path
	templateId := spl[3]
	err = db.View(func(tx *buntdb.Tx) error {
		val, err := tx.Get("template/" + templateId)
		if err != nil {
			return err
		}

		err = json.Unmarshal([]byte(val), &t)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		err = fmt.Errorf("couldn't get template %s: %s", templateId, err)
		return
	}

	// get params from URL path
	params = make(map[string]string)
	for i, paramName := range t.URLParams {
		value := spl[4+i]
		params[paramName] = value
	}

	// get params from querystring
	for paramName, values := range u.Query() {
		value := values[0]
		params[paramName] = value
	}

	return
}

func (t *Template) GetInvoice(
	client *lightning.Client,
	params map[string]string,
) (*Invoice, error) {
	price, err := t.GetInvoicePrice(params)
	if err != nil {
		return nil, fmt.Errorf("error getting price: %w", err)
	}

	now := time.Now()

	invoice := Invoice{
		Id:        cuid.Slug(),
		Template:  t.Id,
		Params:    params,
		Msatoshi:  price,
		Paid:      false,
		CreatedAt: &now,
	}

	metadataHash := sha256.Sum256([]byte(t.EncodedMetadata(params)))
	expirySeconds := 3600

	inv, err := client.CallNamed("lnurlinvoice",
		"msatoshi", invoice.Msatoshi,
		"label", "lnurl/"+invoice.Id,
		"description_hash", hex.EncodeToString(metadataHash[:]),
		"expiry", expirySeconds,
	)
	if err != nil {
		return nil, err
	}
	invoice.Bolt11 = inv.Get("bolt11").String()

	db.Update(func(tx *buntdb.Tx) error {
		j, _ := json.Marshal(invoice)
		tx.Set("template/"+t.Id+"/invoice/"+invoice.Id, string(j),
			&buntdb.SetOptions{Expires: true, TTL: time.Duration(expirySeconds) * time.Second})
		return nil
	})

	return &invoice, nil
}

func (t *Template) GetInvoicePrice(params map[string]string) (int64, error) {
	m := mathcat.New()
	for k, v := range params {
		m.Run(fmt.Sprintf("%s = %v", k, v))
	}
	res, err := m.Run(fmt.Sprintf("%v", t.PriceSatoshi))
	if err != nil {
		return 0, err
	}
	satsf, _ := res.Float64()
	if satsf < 1 {
		return 0, fmt.Errorf("got invalid price: %v", satsf)
	}
	return int64(satsf * 1000), nil
}

func (t *Template) EncodedMetadata(params map[string]string) string {
	plain := t.Metadata["text/plain"]
	rendered := mustache.Render(plain, params)

	j, _ := json.Marshal([][]string{
		{"text/plain", rendered},
	})
	return string(j)
}

type Invoice struct {
	Id        string            `json:"id"`
	Template  string            `json:"template"`
	Params    map[string]string `json:"params"`
	Msatoshi  int64             `json:"msatoshi"`
	Bolt11    string            `json:"bolt11"`
	Paid      bool              `json:"paid"`
	CreatedAt *time.Time        `json:"created_at"`
	PaidAt    *time.Time        `json:"paid_at"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
