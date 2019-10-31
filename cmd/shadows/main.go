package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/zpay32"
)

func main() {
	p := plugin.Plugin{
		Name:    "shadows",
		Version: "v0.1",
		RPCMethods: []plugin.RPCMethod{
			{
				"shadow-invoice",
				"msatoshi id description [expiry]",
				"Create a fake private channel and an invoice linked to it for {msatoshi} with {description} with optional {expiry} seconds (default 1 week).",
				"The fake channel id will be generated from {id}. Both the preimage and the invoice label will be generated from the fake channel id plus a secret derived from your master seed.",
				func(p *plugin.Plugin, params plugin.Params) (resp interface{}, errCode int, err error) {
					// get params
					id := params["id"].(string)
					description := params["description"].(string)
					expiry, _ := params.Float64("expiry")
					if expiry == 0.0 {
						expiry = 604800
					}
					msatoshi, err := params.Float64("msatoshi")
					if err != nil {
						return nil, 400, errors.New("Invalid msatoshi amount.")
					}

					// get our node id and parse public key
					info, err := p.Client.Call("getinfo")
					if err != nil {
						return nil, 500, err
					}
					nodeid := info.Get("id").String()
					bnodeid, err := hex.DecodeString(nodeid)
					if err != nil {
						return nil, 501, err
					}
					pubkey, err := btcec.ParsePubKey(bnodeid, btcec.S256())
					if err != nil {
						return nil, 502, err
					}

					// get current network
					var network string
					switch info.Get("network").String() {
					case "bitcoin":
						network = "bc"
					case "regtest":
						network = "bcrt"
					default:
						network = "tb"
					}

					// conjure a new fake channel from the given id
					sid := sha256.Sum256([]byte(id))
					intid := binary.BigEndian.Uint64(sid[:])
					r := rand.New(rand.NewSource(int64(intid)))
					block := 250000 + r.Intn(250000)
					tx := r.Intn(2000)
					output := r.Intn(5)
					scid := fmt.Sprintf("%dx%dx%d", block, tx, output)
					channelid := block<<40 | tx<<16 | output
					targetprivatekey, err := btcec.NewPrivateKey(btcec.S256())
					if err != nil {
						return nil, 503, err
					}
					targetpubkey := targetprivatekey.PubKey()
					hophint := zpay32.HopHint{
						NodeID:                    pubkey,
						ChannelID:                 uint64(channelid),
						FeeBaseMSat:               0,
						FeeProportionalMillionths: 200,
						CLTVExpiryDelta:           9,
					}

					// use the secret to conjure a preimage and label
					secret, _ := getSecret(p)
					preimage := sha256.Sum256([]byte(secret + ":" + scid + ":preimage"))
					blabel := sha256.Sum256([]byte(secret + ":" + scid + ":label"))
					label := hex.EncodeToString(blabel[:])
					hash := sha256.Sum256(preimage[:])
					hhash := hex.EncodeToString(hash[:])

					// make the encoded invoice
					invoice, err := zpay32.NewInvoice(
						&chaincfg.Params{Bech32HRPSegwit: network},
						hash,
						time.Now(),
						zpay32.Destination(targetpubkey),
						zpay32.RouteHint([]zpay32.HopHint{hophint}),
						zpay32.Description(description),
						zpay32.Amount(lnwire.MilliSatoshi(msatoshi)),
						zpay32.Expiry(time.Second*time.Duration(expiry)),
					)
					if err != nil {
						return nil, 504, err
					}

					bolt11, err := invoice.Encode(zpay32.MessageSigner{
						SignCompact: func(hash []byte) ([]byte, error) {
							return btcec.SignCompact(btcec.S256(),
								targetprivatekey, hash, true)
						},
					})
					if err != nil {
						return nil, 506, err
					}

					return map[string]interface{}{
						"bolt11":       bolt11,
						"label":        label,
						"payment_hash": hhash,
						"expires_at":   (time.Now().Add(time.Duration(expiry) * time.Second)).Unix(),
					}, 0, nil
				},
			},
		},
		Hooks: []plugin.Hook{
			{
				"htlc_accepted",
				func(p *plugin.Plugin, params plugin.Params) (resp interface{}) {
					next, ok := getNextChannel(params["onion"])
					if !ok {
						return map[string]interface{}{"result": "continue"}
					}

					nextp := strings.Split(next, "x")
					if len(nextp) != 3 {
						return map[string]interface{}{"result": "continue"}
					}

					// use the channel to get the preimage and label
					secret, _ := getSecret(p)
					preimage := sha256.Sum256([]byte(secret + ":" + next + ":preimage"))
					blabel := sha256.Sum256([]byte(secret + ":" + next + ":label"))
					label := hex.EncodeToString(blabel[:])
					hpreimage := hex.EncodeToString(preimage[:])
					hash := sha256.Sum256(preimage[:])
					hhash := hex.EncodeToString(hash[:])

					htlchash := params["htlc"].(map[string]interface{})["payment_hash"].(string)
					if strings.ToLower(htlchash) == hhash {
						p.Log("got payment " + label)

						return map[string]interface{}{
							"result":      "resolve",
							"payment_key": hpreimage,
						}
					}

					return map[string]interface{}{"result": "continue"}
				},
			},
		},
	}

	p.Run()
}

func getSecret(p *plugin.Plugin) (secret string, err error) {
	sk, err := p.Client.GetCustomKey(0, "shadow-secret")
	if err != nil {
		return
	}
	return hex.EncodeToString(sk.PubKey().SerializeCompressed()), nil
}

func getNextChannel(onion interface{}) (nextChannel string, ok bool) {
	if o, ok := onion.(map[string]interface{}); ok {
		if perHopV0, ok := o["per_hop_v0"]; ok {
			if phv0, ok := perHopV0.(map[string]interface{}); ok {
				if shortChannelId, ok := phv0["short_channel_id"]; ok {
					if scid, ok := shortChannelId.(string); ok {
						return scid, true
					}
				}
			}
		}
	}
	return
}

type Signer btcec.PrivateKey
