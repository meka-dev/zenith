package api_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"zenith/api"
	"zenith/store/memstore"

	"github.com/go-kit/log"
)

func TestProxyCORS(t *testing.T) {
	store := memstore.NewStore()
	networkToURI := map[string]string{}
	manager := api.NewStoreReverseProxyManager(store, networkToURI)
	logger := log.NewNopLogger()
	proxy, err := api.NewProxy(manager, logger)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/v0/auction", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	for key, wants := range map[string][]string{
		"access-control-allow-origin":  {"*"},
		"access-control-allow-methods": {"GET", "POST"},
		"access-control-allow-headers": {"content-type", "accept", api.ChainIDHeaderKey},
	} {
		val := rec.Result().Header.Get(key)
		toks := strings.Split(val, ",")
		index := map[string]bool{}
		for _, tok := range toks {
			index[strings.TrimSpace(tok)] = true
		}
		for _, want := range wants {
			if !index[want] {
				t.Errorf("%s: %s -- missing %s", key, val, want)
			}
		}
	}
}
