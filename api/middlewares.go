package api

import (
	"fmt"
	"mekapi/trc/eztrc"
	"net/http"
	"strings"

	"github.com/go-kit/log"
)

func corsHeadersMiddleware(next http.Handler) http.Handler {
	var (
		allowOrigin  = "*"
		allowMethods = strings.Join([]string{"GET", "POST"}, ", ")
		allowHeaders = strings.Join([]string{"content-type", "accept", ChainIDHeaderKey}, ", ")
	)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("access-control-allow-origin", allowOrigin) // we have users calling Zenith from JS in browsers
		w.Header().Set("access-control-allow-methods", allowMethods)
		w.Header().Set("access-control-allow-headers", allowHeaders)
		next.ServeHTTP(w, r)
	})
}

func panicRecoveryMiddleware(logger log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					eztrc.Errorf(r.Context(), "PANIC: %v", v)
					respondError(w, r, fmt.Errorf("panic: %v", v), 599, logger)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
