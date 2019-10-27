package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"regexp"
)

var nonLetters = regexp.MustCompile(`\W+`)

func hmacStr(key, data string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(data))
	b64 := base64.StdEncoding.EncodeToString(h.Sum(nil))
	return nonLetters.ReplaceAllString(b64, "")
}

func pathExists(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	return true
}
