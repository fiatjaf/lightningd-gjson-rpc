package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	lightning "github.com/fiatjaf/lightningd-gjson-rpc"
)

func handleRPC(w http.ResponseWriter, r *http.Request) {
	var req lightning.JSONRPCMessage
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		w.WriteHeader(400)
		return
	}

	// check permissions
	if permissions, ok := r.Context().Value("permissions").(map[string]bool); ok {
		if len(permissions) > 0 {
			if _, allowed := permissions[req.Method]; !allowed {
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
				Request:  req,
			})
		}

		return
	}

	w.Header().Set("Content-Type", "application/json")

	// if we have a "Range" header, try to filter the response
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		spl := strings.Split(rangeHeader, "=")
		if len(spl) != 2 {
			goto sendEverythingWithoutRange
		}

		unit := spl[0]

		spl = strings.Split(spl[1], "-")
		if len(spl) != 2 {
			goto sendEverythingWithoutRange
		}

		from, err1 := strconv.Atoi(spl[0])
		to, err2 := strconv.Atoi(spl[1])
		var transformEntries func([]json.RawMessage) []json.RawMessage
		if spl[0] == "" {
			// suffix-length, only "to" should be valid
			if err2 != nil {
				goto sendEverythingWithoutRange
			}
			// we go from `-to` to the end
			transformEntries = func(entries []json.RawMessage) []json.RawMessage {
				if len(entries) < to {
					to = len(entries)
				}

				return entries[len(entries)-to:]
			}
		} else {
			// range-start - range-end, both should be valid
			if err1 != nil || err2 != nil {
				goto sendEverythingWithoutRange
			}
			// go from `from` to `to`
			transformEntries = func(entries []json.RawMessage) []json.RawMessage {
				if from < 0 {
					from = 0
				}
				if len(entries) < to {
					to = len(entries)
				}

				return entries[from:to]
			}
		}

		var response map[string][]json.RawMessage
		err = json.Unmarshal(respbytes, &response)
		if err != nil {
			goto sendEverythingWithoutRange
		}

		if entries, ok := response[unit]; ok {
			response[unit] = transformEntries(entries)
		}

		json.NewEncoder(w).Encode(response)
		return
	}

sendEverythingWithoutRange:
	w.Write(respbytes)
}

type LightningError struct {
	Type     string                   `json:"type"`
	Name     string                   `json:"name"`
	Message  string                   `json:"message"`
	Code     int                      `json:"code"`
	FullType string                   `json:"fullType"`
	Request  lightning.JSONRPCMessage `json:"request"`
}
