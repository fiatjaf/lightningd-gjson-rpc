package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
	"github.com/hoisie/mustache"
	"github.com/lucsky/cuid"
	"github.com/soudy/mathcat"
	"github.com/tidwall/buntdb"
)

type Template struct {
	Id            string            `json:"id"`
	PathParams    []string          `json:"path_params,omitempty"`
	QueryParams   map[string]bool   `json:"query_params,omitempty"`
	Metadata      map[string]string `json:"metadata"`
	PriceSatoshi  interface{}       `json:"price_satoshi"`
	Webhook       string            `json:"webhook"`
	SecretCodeKey string            `json:"secret_code_key,omitempty"`
}

func (t *Template) MakeURL(baseURL string, hmacKey []byte, params map[string]string) string {
	u, _ := url.Parse(baseURL + "/" + t.Id + "/")

	// add path params
	path := make([]string, len(t.PathParams))
	for i, key := range t.PathParams {
		value, _ := params[key]
		path[i] = fmt.Sprint(value)
	}
	if len(path) > 0 {
		u.Path += strings.Join(path, "/")
	}

	// add querystring params
	qs := url.Values{}
	for key, _ := range t.QueryParams {
		if value, ok := params[key]; ok {
			qs.Set(key, fmt.Sprint(value))
		}
	}

	// add hmac
	mac := hmac.New(sha256.New, hmacKey)
	mac.Write([]byte(u.Path))
	qs.Set("hmac", hex.EncodeToString(mac.Sum(nil)))

	u.RawQuery = qs.Encode()
	return u.String()
}

func FromURL(u *url.URL, hmacKey []byte) (t Template, params map[string]string, err error) {
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}

	qs := u.Query()
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
	for i, paramName := range t.PathParams {
		value := spl[4+i]
		params[paramName] = value
	}

	// verify path hmac
	code, _ := hex.DecodeString(qs.Get("hmac"))
	mac := hmac.New(sha256.New, hmacKey)
	mac.Write([]byte(u.Path))
	if !hmac.Equal(code, mac.Sum(nil)) {
		err = errors.New("invalid lnurl: hmac doesn't match")
		return
	}
	qs.Del("hmac")

	// get params from querystring
	for paramName, values := range qs {
		value := values[0]
		params[paramName] = value
	}

	return
}

func (t *Template) GetInvoice(
	p *plugin.Plugin,
	params map[string]string,
) (invoice *Invoice, err error) {
	price, err := t.GetInvoicePrice(params)
	if err != nil {
		return nil, fmt.Errorf("error getting price: %w", err)
	}

	now := time.Now()

	invoice = &Invoice{
		Id:        cuid.Slug(),
		Template:  t.Id,
		Params:    params,
		Msatoshi:  price,
		Paid:      false,
		CreatedAt: &now,
	}

	metadataHash := sha256.Sum256([]byte(t.EncodedMetadata(params)))
	expirySeconds := 3600
	preimage := make([]byte, 32)
	if _, err = io.ReadFull(rand.Reader, preimage); err != nil {
		return nil, err
	}
	preimageStr := hex.EncodeToString(preimage)

	inv, err := p.Client.CallNamed("lnurlinvoice",
		"preimage", preimageStr,
		"msatoshi", invoice.Msatoshi,
		"label", "lnurl/"+invoice.Id,
		"description_hash", hex.EncodeToString(metadataHash[:]),
		"expiry", expirySeconds,
	)
	if err != nil {
		return nil, err
	}
	invoice.Preimage = preimageStr
	invoice.Bolt11 = inv.Get("bolt11").String()

	db.Update(func(tx *buntdb.Tx) error {
		j, _ := json.Marshal(invoice)
		tx.Set("template/"+t.Id+"/invoice/"+invoice.Id, string(j),
			&buntdb.SetOptions{Expires: true, TTL: time.Duration(expirySeconds) * time.Second})
		return nil
	})

	return invoice, nil
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
	Preimage  string            `json:"preimage"`
	Bolt11    string            `json:"bolt11"`
	Paid      bool              `json:"paid"`
	CreatedAt *time.Time        `json:"created_at"`
	PaidAt    *time.Time        `json:"paid_at"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
