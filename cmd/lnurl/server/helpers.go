package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/fiatjaf/lightningd-gjson-rpc"
	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
)

func getPath(p *plugin.Plugin, key string) string {
	if givenpath, err := p.Args.String(key); err == nil {
		if !filepath.IsAbs(givenpath) {
			return filepath.Join(filepath.Dir(p.Client.Path), givenpath)
		} else {
			return givenpath
		}
	}

	return ""
}

func pathExists(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	return true
}

func getDefaultHMACKey(client *lightning.Client) []byte {
	key, err := client.GetCustomBytes(0, "lnurl-server-hmac-key")
	if err != nil {
		panic("couldn't get key from hsm_secret! please set manually.")
	}
	return key
}

func generateCode(template *Template, inv *Invoice) string {
	now5min := time.Now().Unix() / (60 * 5)
	h := hmac.New(sha256.New, []byte(template.SecretCodeKey))
	h.Write([]byte(strconv.FormatInt(now5min, 10)))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))[:6]
}
