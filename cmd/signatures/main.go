package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/btcsuite/btcd/btcec"
	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
)

func main() {
	p := plugin.Plugin{
		Name:    "signatures",
		Dynamic: true,
		RPCMethods: []plugin.RPCMethod{
			{
				Name:            "sign",
				Usage:           "message",
				Description:     "Signs a string {message} with the node's public key.",
				LongDescription: `If {message} is a valid 32-byte hex-encoded string, it will be signed as it is, otherwise it will be hashed with sha256 first. The response will include a hex-encoded "signature" and a boolean "hashed" indicating if the given message was hashed before signing.`,
				Handler: func(p *plugin.Plugin, params map[string]interface{}) (resp interface{}, errCode int, err error) {
					sk, err := p.Client.GetPrivateKey()
					if err != nil {
						errCode = 2
						return
					}

					hash, wasHashed := getHashedMessage(params)

					signature, err := sk.Sign(hash)
					if err != nil {
						errCode = 3
						return
					}

					return map[string]interface{}{
						"signature":  hex.EncodeToString(signature.Serialize()),
						"was_hashed": wasHashed,
						"hash":       hex.EncodeToString(hash),
						"pubkey":     hex.EncodeToString(sk.PubKey().SerializeCompressed()),
					}, 0, nil
				},
			},
			{
				Name:            "verify",
				Usage:           "message signature [pubkey]",
				Description:     "Verifies a string {message} against the given {pubkey}.",
				LongDescription: "{pubkey} is expected to be 33-byte, hex-encoded, and {signature} also hex-encoded. {message} can be either a 32-byte hex-encoded string or a full string message; in the first case it will be used as it is, in the second it will be first hashed with sha256 and then verified.",
				Handler: func(p *plugin.Plugin, params map[string]interface{}) (resp interface{}, errCode int, err error) {
					bsig, err := hex.DecodeString(params["signature"].(string))
					if err != nil {
						errCode = 3
						err = errors.New("Failed to decode signature: " + err.Error())
						return
					}

					hash, wasHashed := getHashedMessage(params)
					curve := btcec.S256()

					if ipubkey, given := params["pubkey"]; given {
						// pubkey given, see if it matches the signature
						bpubkey, err := hex.DecodeString(ipubkey.(string))
						if err != nil {
							return nil, 4, errors.New("Failed to decode pubkey " + ipubkey.(string) + ": " + err.Error())
						}

						pubkey, err := btcec.ParsePubKey(bpubkey, curve)
						if err != nil {
							return nil, 4, errors.New("Failed to parse pubkey: " + err.Error())
						}

						signature, err := btcec.ParseSignature(bsig, curve)
						if err != nil {
							return nil, 5, errors.New("Failed to parse signature: " + err.Error())
						}

						return map[string]interface{}{
							"valid":      signature.Verify(hash, pubkey),
							"was_hashed": wasHashed,
							"hash":       hex.EncodeToString(hash),
						}, 0, nil

					} else {
						// pubkey not given, try to verify anyway and recover a public key
						if pk, _, err := btcec.RecoverCompact(curve, bsig, hash); err == nil {
							return map[string]interface{}{
								"valid":            true,
								"recovered_pubkey": hex.EncodeToString(pk.SerializeCompressed()),
								"was_hashed":       wasHashed,
								"hash":             hex.EncodeToString(hash),
							}, 0, nil
						} else {
							p.Log(err)

							return map[string]interface{}{
								"valid":      false,
								"was_hashed": wasHashed,
								"hash":       hex.EncodeToString(hash),
							}, 0, nil
						}
					}
				},
			},
		},
	}

	p.Run()
}

func getHashedMessage(params map[string]interface{}) (hash []byte, wasHashed bool) {
	message := params["message"].(string)
	hash, err := hex.DecodeString(message)
	if err != nil {
		// not a valid hash, so hash the full message
		hash256 := sha256.Sum256([]byte(message))
		hash = hash256[:]
		wasHashed = true
	} else {
		// is a valid hex-decoded 32-byte string, assume it's the hash to be signed/verified
	}

	return
}
