package main

import (
	"fmt"
	"strings"
)

type Keys map[string]map[string]bool

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
			keys[key] = make(map[string]bool)
			continue
		}
		if len(parts) > 2 {
			// invalid
			continue
		}

		perms := parts[1]
		set := make(map[string]bool)

		for _, method := range strings.Split(perms, ",") {
			method = strings.TrimSpace(method)
			set[method] = true
		}

		keys[key] = set
	}

	return keys, err
}

func (keys Keys) Summary() (string, int) {
	out := make([]string, len(keys))
	i := 0
	for key, permissions := range keys {
		listed := "full-access"
		if len(permissions) > 0 {
			listed = fmt.Sprintf("%d permission", len(permissions))
		}
		out[i] = key + " (" + listed + ")"
		i++
	}

	if i == 0 {
		return "none.", i
	}

	return strings.Join(out, ", "), i
}
