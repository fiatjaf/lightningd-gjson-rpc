package lightning

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg"
	decodepay "github.com/fiatjaf/ln-decodepay"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/zpay32"
)

const DESCRIPTION_HASH_DESCRIPTION_PREFIX = "with description_hash: "

func (ln *Client) InvoiceWithDescriptionHash(
	label string,
	msatoshi int64,
	descriptionHash []byte,
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

	description_hash := hex.EncodeToString(descriptionHash)

	// create an invoice at the node so it expects for a payment at this hash
	// we won't expose this, but it will still get paid
	params := map[string]interface{}{
		"label":       label,
		"msatoshi":    msatoshi,
		"preimage":    hex.EncodeToString(preimage),
		"description": DESCRIPTION_HASH_DESCRIPTION_PREFIX + description_hash,
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
	return ln.TranslateInvoiceWithDescriptionHash(bolt11)
}

func (ln *Client) TranslateInvoiceWithDescriptionHash(bolt11 string) (string, error) {
	invoice, err := zpay32.Decode(bolt11, decodepay.ChainFromCurrency(bolt11[2:]))
	if err != nil {
		return "", fmt.Errorf("failed to decode bolt11: %w", err)
	}

	// grab description_hash from description
	if invoice.Description == nil {
		return "", errors.New("given bolt11 doesn't have a text description")
	}
	descriptionHashHex := (*invoice.Description)[len(DESCRIPTION_HASH_DESCRIPTION_PREFIX):]
	descriptionHash, err := hex.DecodeString(descriptionHashHex)
	if err != nil {
		return "", fmt.Errorf("given bolt11 doesn't have a valid description_hash in its text description: %w", err)
	}
	dhash32 := as32(descriptionHash)

	invoice.Description = nil
	invoice.Destination = nil
	invoice.DescriptionHash = &dhash32

	// finally sign this new invoice
	privKey, err := ln.GetPrivateKey()
	if err != nil {
		return "", err
	}

	translatedBolt11, err := invoice.Encode(zpay32.MessageSigner{
		SignCompact: func(hash []byte) ([]byte, error) {
			return btcec.SignCompact(btcec.S256(), privKey, hash, true)
		},
	})

	return translatedBolt11, err
}

func (ln *Client) InvoiceWithShadowRoute(
	msatoshi int64,
	descriptionOrHash interface{}, /* can be either a string (description) or a []byte (description_hash) */
	ppreimage *[]byte,
	pprivateKey **btcec.PrivateKey,
	pexpiry *time.Duration,
	baseFee uint32,
	ppmFee uint32,
	cltvExpiryDelta uint16,
	channelId uint64,
) (bolt11 string, paymentHash string, err error) {
	// create a random preimage if one is not given
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
	hash := sha256.Sum256(preimage)
	paymentHash = hex.EncodeToString(hash[:])

	// params for invoice creation
	params := make([]func(*zpay32.Invoice), 5, 6)

	// payment secret can be anything
	params[0] = zpay32.PaymentAddr([32]byte{
		1, 2, 3, 4, 5, 6, 7, 8,
		9, 10, 11, 12, 13, 14, 15, 16,
		17, 18, 19, 20, 21, 22, 23, 24,
		25, 26, 27, 28, 29, 30, 31, 32,
	})

	// set expiry to 7 days if not given
	if pexpiry != nil {
		params[1] = zpay32.Expiry(*pexpiry)
	} else {
		params[1] = zpay32.Expiry(time.Duration(time.Hour * 24 * 7))
	}

	// set the description or description_hash
	switch v := descriptionOrHash.(type) {
	case string:
		// it's a plain description (`d`)
		params[2] = zpay32.Description(v)
	case []byte:
		// it's a description_hash (`h`)
		descriptionHash := as32(v)
		params[2] = zpay32.DescriptionHash(descriptionHash)
	}

	// set amount if not zero
	if msatoshi > 0 {
		params = append(params, zpay32.Amount(lnwire.MilliSatoshi(msatoshi)))
	}

	// set the shadow route hint with the public key of our real node
	info, _ := ln.Call("getinfo")
	nodeIdBytes, _ := hex.DecodeString(info.Get("id").String())
	pubKey, err := btcec.ParsePubKey(nodeIdBytes, btcec.S256())
	if err != nil {
		return
	}
	params[3] = zpay32.RouteHint([]zpay32.HopHint{
		{
			NodeID:                    pubKey,
			ChannelID:                 channelId,
			FeeBaseMSat:               baseFee,
			FeeProportionalMillionths: ppmFee,
			CLTVExpiryDelta:           cltvExpiryDelta,
		},
	})

	params[4] = zpay32.Features(&lnwire.FeatureVector{
		RawFeatureVector: lnwire.NewRawFeatureVector(
			lnwire.PaymentAddrOptional,
			lnwire.TLVOnionPayloadOptional,
		),
	})

	// create the invoice
	invoice, err := zpay32.NewInvoice(&chaincfg.MainNetParams,
		hash, time.Now(), params...)
	if err != nil {
		return
	}

	// create a private key if one is not given, we need it to sign
	var privateKey *btcec.PrivateKey
	if pprivateKey != nil {
		privateKey = *pprivateKey
	} else {
		randomBytes := make([]byte, 32)
		_, err = rand.Read(randomBytes)
		if err != nil {
			return
		}
		privateKey, _ = btcec.PrivKeyFromBytes(btcec.S256(), randomBytes)
	}

	bolt11, err = invoice.Encode(zpay32.MessageSigner{
		SignCompact: func(hash []byte) ([]byte, error) {
			return btcec.SignCompact(btcec.S256(), privateKey, hash, true)
		},
	})

	return
}
