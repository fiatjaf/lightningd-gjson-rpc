package main

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/fiatjaf/lightningd-gjson-rpc"
)

func handleRPC(w http.ResponseWriter, r *http.Request) {
	var req lightning.JSONRPCMessage
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		w.WriteHeader(400)
		return
	}

	// check permissions
	if permissions, ok := r.Context().Value("permissions").(PermissionSet); ok {
		// if there are any allowed_methods, ignore disallowed_methods
		// setting and disallow anything that is not explicitly allowed.
		// otherwise only disallow what is explicitly disallowed.
		if len(permissions.Allow) > 0 {
			if _, allowed := permissions.Allow[req.Method]; !allowed {
				w.WriteHeader(401)
				return
			}
		} else {
			if _, disallowed := permissions.Disallow[req.Method]; disallowed {
				w.WriteHeader(401)
				return
			}
		}
	}

	// actually do the call
	respbytes, err := r.Context().Value("client").(*lightning.Client).CallMessageRaw(time.Second*30, req)
	if err != nil {
		w.WriteHeader(500)

		if cmderr, ok := err.(lightning.ErrorCommand); ok {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(LightningError{
				Type:     "lightning",
				Name:     "LightningError",
				Message:  cmderr.Message,
				Code:     cmderr.Code,
				FullType: "lightning",
			})
		}

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(respbytes)
}

type LightningError struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Message  string `json:"message"`
	Code     int    `json:"code"`
	FullType string `json:"fullType"`
}
