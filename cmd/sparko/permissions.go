package main

import (
	"fmt"
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
		key := strings.TrimSpace(parts[0])
		if key == "" {
			continue
		}

		if len(parts) == 1 {
			// it has all permissions
			keys[key] = PermissionSet{}
			continue
		}
		if len(parts) > 2 {
			// invalid
			continue
		}

		perms := parts[1]
		set := PermissionSet{
			Allow:    make(map[string]bool),
			Disallow: make(map[string]bool),
		}
		for _, methodKey := range strings.Split(perms, ",") {
			methodKey = strings.TrimSpace(methodKey)
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

func (keys Keys) String() string {
	out := make([]string, len(keys))
	i := 0
	for key, permissions := range keys {
		listed := "full-access"
		if len(permissions.Allow) > 0 {
			listed = fmt.Sprintf("%d whitelisted", len(permissions.Allow))
		} else if len(permissions.Disallow) > 0 {
			listed = fmt.Sprintf("%d blacklisted", len(permissions.Disallow))
		}

		out[i] = key + " (" + listed + ")"
		i++
	}
	return strings.Join(out, ", ")
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
