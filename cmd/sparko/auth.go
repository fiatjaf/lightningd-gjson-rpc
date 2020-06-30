package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func defaultAuth(r *http.Request) error {
	if accessKey == "" {
		return errors.New("no default login credentials set")
	}
	if r.Header.Get("X-Access") == accessKey {
		return nil
	}
	if r.URL.Query().Get("access-key") == accessKey {
		return nil
	}

	// try to get basic auth
	v := r.Header.Get("Authorization")
	parts := strings.Split(v, " ")
	if len(parts) == 2 {
		creds, err := base64.StdEncoding.DecodeString(parts[1])
		if err == nil {
			if string(creds) == login {
				return nil
			}
		}
	}

	return fmt.Errorf("Invalid access key.")
}

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")

		if path == "" || path == "rpc" || path == "stream" {
			// default key / login
			if err := defaultAuth(r); err == nil {
				// set cookie
				user := strings.Split(login, ":")[0]
				if encoded, err := scookie.Encode("user", user); err == nil {
					cookie := &http.Cookie{
						Name:     "user",
						Value:    encoded,
						Secure:   true,
						HttpOnly: true,
						SameSite: http.SameSiteStrictMode,
						MaxAge:   2592000,
					}
					http.SetCookie(w, cookie)
				}

				next.ServeHTTP(w, r)
				return
			}

			// extra keys -- only access the /rpc and /stream endpoints
			if path == "rpc" || path == "stream" {
				for key, permissions := range keys {
					if r.Header.Get("X-Access") == key ||
						r.URL.Query().Get("access-key") == key {

						r = r.WithContext(context.WithValue(
							r.Context(),
							"permissions", permissions,
						))

						next.ServeHTTP(w, r)
						return
					}
				}
			}

			w.Header().Set("WWW-Authenticate", `Basic realm="Private Area"`)
			w.WriteHeader(401)
			return
		}

		// if you know where the manifest is you can have it
		if manifestKey != "" && path == "manifest-"+manifestKey+"/manifest.json" {
			r.URL.Path = "/manifest/manifest.json"
		}

		next.ServeHTTP(w, r)
		return
	})
}
