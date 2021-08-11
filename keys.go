package lightning

import (
	"crypto/sha256"
	"errors"
	"io"
	"io/ioutil"
	"path/filepath"

	"github.com/btcsuite/btcd/btcec"
	"golang.org/x/crypto/hkdf"
)

// GetPrivateKey gets the custom key with the parameters that return the node's master key (0 and "nodeid")
func (ln *Client) GetPrivateKey() (sk *btcec.PrivateKey, err error) {
	return ln.GetCustomKey(0, "nodeid")
}

// GetCustomKey  reads the hsm_secret file in the same directory as the lightning-rpc socket
// (given by Client.Path) and derives the node private key from it.
func (ln *Client) GetCustomKey(
	index byte,
	label string,
) (sk *btcec.PrivateKey, err error) {
	key, err := ln.GetCustomBytes(index, label)
	if err != nil {
		return nil, err
	}
	sk, _ = btcec.PrivKeyFromBytes(btcec.S256(), key)
	return
}

func (ln *Client) GetCustomBytes(
	index byte,
	label string,
) (b []byte, err error) {
	if ln.Path == "" {
		return nil, errors.New("Path must be set so we know where the lightning folder is.")
	}

	lightningdir := ln.LightningDir
	if lightningdir == "" {
		lightningdir = filepath.Dir(ln.Path)
	}
	hsmsecretpath := filepath.Join(lightningdir, "hsm_secret")

	hash := sha256.New
	secret, err := ioutil.ReadFile(hsmsecretpath)
	if err != nil {
		return
	}
	salt := []byte{index}
	info := []byte(label)

	hkdf := hkdf.New(hash, secret, salt, info)

	b = make([]byte, 32)
	_, err = io.ReadFull(hkdf, b)
	if err != nil {
		return
	}

	return b, nil
}
