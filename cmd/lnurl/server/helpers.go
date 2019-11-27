package server

import (
	"os"
	"path/filepath"

	lightning "github.com/fiatjaf/lightningd-gjson-rpc"
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
