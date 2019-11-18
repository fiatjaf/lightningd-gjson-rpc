package lightning

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/btcsuite/btcd/btcec"
	decodepay "github.com/fiatjaf/ln-decodepay"
	"github.com/lightningnetwork/lnd/zpay32"
)

func (ln *Client) InvoiceWithDescriptionHash(
	label string,
	msatoshi int64,
	plainDescription string,
	ppreimage *[]byte,
	pexpiry *time.Duration,
) (bolt11 string, err error) {
	var preimage []byte
	if ppreimage != nil {
		preimage = *ppreimage
	} else {
		preimage = make([]byte, 32)
		_, err = rand.Read(preimage)
		if err != nil {
			return
		}
	}

	dhash := sha256.Sum256([]byte(plainDescription))
	dhash32 := as32(dhash[:])
	description_hash := hex.EncodeToString(dhash[:])

	// create an invoice at the node so it expects for a payment at this hash
	// we won't expose this, but it will still get paid
	params := map[string]interface{}{
		"label":       label,
		"msatoshi":    msatoshi,
		"preimage":    hex.EncodeToString(preimage),
		"description": "with description_hash: " + description_hash,
	}

	if pexpiry != nil {
		params["expiry"] = *pexpiry / time.Second
	}

	inv, err := ln.Call("invoice", params)
	if err != nil {
		return
	}

	// now create another invoice, this time with the desired description_hash instead
	bolt11 = inv.Get("bolt11").String()
	invoice, err := zpay32.Decode(bolt11, decodepay.ChainFromCurrency(bolt11[2:]))
	if err != nil {
		return
	}
	invoice.Description = nil
	invoice.Destination = nil
	invoice.DescriptionHash = &dhash32

	// finally sign this new invoice
	privKey, err := ln.GetPrivateKey()
	if err != nil {
		return
	}

	bolt11, err = invoice.Encode(zpay32.MessageSigner{
		SignCompact: func(hash []byte) ([]byte, error) {
			return btcec.SignCompact(btcec.S256(), privKey, hash, true)
		},
	})
	return
}
