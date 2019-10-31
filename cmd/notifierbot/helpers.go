package main

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
	"github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/zpay32"
)

var bolt11regex = regexp.MustCompile(`.*?((lnbcrt|lntb|lnbc)([0-9]{1,}[a-z0-9]+){1})`)

func searchForInvoice(message *tgbotapi.Message) (bolt11 string, ok bool) {
	text := message.Text
	if text == "" {
		text = message.Caption
	}

	text = strings.ToLower(text)
	results := bolt11regex.FindStringSubmatch(text)

	if len(results) == 0 {
		return
	}

	return results[1], true
}

func getOnionData(onion interface{}) (nextChannel string, nextAmount int, ok bool) {
	if o, ok := onion.(map[string]interface{}); ok {
		if perHopV0, ok := o["per_hop_v0"]; ok {
			if phv0, ok := perHopV0.(map[string]interface{}); ok {
				var ok1, ok2 bool
				if shortChannelId, ok := phv0["short_channel_id"]; ok {
					if scid, ok := shortChannelId.(string); ok {
						nextChannel = scid
						ok1 = true
					}
				}
				if forwardAmount, ok := phv0["forward_amount"]; ok {
					if amt, ok := forwardAmount.(string); ok {
						nextAmount, err = strconv.Atoi(amt[:len(amt)-4])
						if err == nil {
							ok2 = true
						}
					}
				}
				return nextChannel, nextAmount, ok1 && ok2
			}
		}
	}
	return
}

func makeInvoice(p *plugin.Plugin, originalInvoice string) (hash string, newInvoice string, err error) {
	// parse public key
	nodeid := info.Get("id").String()
	bnodeid, err := hex.DecodeString(nodeid)
	if err != nil {
		return
	}
	pubkey, err := btcec.ParsePubKey(bnodeid, btcec.S256())
	if err != nil {
		return
	}

	// get current network
	var currency string
	switch info.Get("network").String() {
	case "bitcoin":
		currency = "bc"
	case "regtest":
		currency = "bcrt"
	default:
		currency = "tb"
	}

	// parse original invoice
	inv, err := p.Client.Call("decodepay", originalInvoice)
	if err != nil {
		return
	}
	msatoshi := inv.Get("msatoshi").Int()
	payee := inv.Get("payee").String()
	hash = inv.Get("payment_hash").String()
	description := inv.Get("description").String()
	createdAt := inv.Get("created_at").Int()
	expiry := inv.Get("expiry").Int()

	// turn hash into [32]byte
	var hash32 [32]byte
	if bhash, err := hex.DecodeString(hash); err == nil && len(bhash) == 32 {
		for i := 0; i < 32; i++ {
			hash32[i] = bhash[i]
		}
	} else {
		err = errors.New("hash is malformed")
	}

	// check currency
	if currency != inv.Get("currency").String() {
		err = errors.New("mismatched currencies: " + currency + " != " + inv.Get("currency").String())
		return
	}

	// check expiry
	if createdAt+expiry < time.Now().Add(time.Second*7200).Unix() {
		err = errors.New("invoice is expired or going to expire too soon!")
		return
	}

	// conjure a node and fake channel from ournodeid+theirnodeid
	seed, err := hex.DecodeString(nodeid + payee)
	if err != nil {
		return
	}
	intid := binary.BigEndian.Uint64(seed[:])
	r := rand.New(rand.NewSource(int64(intid)))
	block := 250000 + r.Intn(250000)
	tx := r.Intn(2000)
	output := r.Intn(5)
	channelid := block<<40 | tx<<16 | output

	targetprivatekey, err := p.Client.GetCustomKey(0, payee)
	if err != nil {
		return
	}
	targetpubkey := targetprivatekey.PubKey()
	hophint := zpay32.HopHint{
		NodeID:                    pubkey,
		ChannelID:                 uint64(channelid),
		FeeBaseMSat:               0,
		FeeProportionalMillionths: 3000,
		CLTVExpiryDelta:           144,
	}

	// make the encoded invoice
	invoice, err := zpay32.NewInvoice(
		&chaincfg.Params{Bech32HRPSegwit: currency},
		hash32,
		time.Unix(createdAt, 0),
		zpay32.Destination(targetpubkey),
		zpay32.RouteHint([]zpay32.HopHint{hophint}),
		zpay32.Description(description),
		zpay32.Amount(lnwire.MilliSatoshi(msatoshi)),
		zpay32.Expiry(time.Second*time.Duration(expiry)),
	)
	if err != nil {
		return
	}

	bolt11, err := invoice.Encode(zpay32.MessageSigner{
		SignCompact: func(hash []byte) ([]byte, error) {
			return btcec.SignCompact(btcec.S256(),
				targetprivatekey, hash, true)
		},
	})
	if err != nil {
		return
	}

	return hash, bolt11, nil
}
