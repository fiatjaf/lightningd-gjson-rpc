package main

import (
	"strings"
)

type Keys map[string]PermissionSet

type PermissionSet struct {
	Allow    map[string]bool
	Disallow map[string]bool
}

func readPermissionsConfig(configstr string) (Keys, error) {
	keys := make(Keys)

	for _, keyentry := range strings.Split(configstr, ";") {
		parts := strings.Split(keyentry, ":")
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		perms := parts[1]

		set := PermissionSet{
			Allow:    make(map[string]bool),
			Disallow: make(map[string]bool),
		}
		for _, methodKey := range strings.Split(perms, ",") {
			methods := []string{methodKey[1:]}
			if groupmethods, ok := groups[methodKey[1:]]; ok {
				methods = groupmethods
			}

			if strings.HasPrefix(methodKey, "+") {
				for _, methodName := range methods {
					set.Allow[methodName] = true
				}
			} else if strings.HasPrefix(methodKey, "-") {
				for _, methodName := range methods {
					set.Disallow[methodName] = true
				}
			}
		}

		keys[key] = set
	}

	return keys, err
}

// predefined groups
var groups = map[string][]string{
	"readonly": {
		"getinfo",
		"listforwards",
		"listfunds",
		"listsendpays",
		"listinvoices",
		"listnodes",
		"listpeers",
		"listchannels",
		"getroute",
		"feerates",
		"waitinvoice",
		"waitanyinvoice",
		"decodepay",
		"paystatus",
		"waitsendpay",
	},
	"invoice": {
		"invoice",
		"waitinvoice",
	},
}
