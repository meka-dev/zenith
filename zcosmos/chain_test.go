package zcosmos

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestChain_ValidatePaymentAddress(t *testing.T) {
	ctx := context.Background()

	var (
		netConf = NetworkConfig{
			Network:             "osmosis",
			Bech32PrefixAccAddr: "osmo",
			StallThreshold:      time.Hour,
		}
		chainID    = "osmosis-1"
		rpcAddrs   = []string(nil)
		httpClient = http.DefaultClient
	)
	chain, err := NewChain(netConf, chainID, rpcAddrs, httpClient)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		addr string
		good bool
	}{
		{"", false},
		{"osmo", false},
		{"osmovaloper", false},
		{"osmovaloper196ax4vc0lwpxndu9dyhvca7jhxp70rmcmmarz7FOOBAR", false},
		{"osmovaloper196ax4vc0lwpxndu9dyhvca7jhxp70rmcmmarz7foobar", false},
		{"osmovaloper196ax4vc0lwpxndu9dyhvca7jhxp70rmcmmarz78", false},
		{"osmovaloper196ax4vc0lwpxndu9dyhvca7jhxp70rmcmmarz7", true},
		{"osmo1clpqr4nrk4khgkxj78fcwwh6dl3uw4epasmvnj", true},
	} {
		t.Run(tc.addr, func(t *testing.T) {
			err := chain.ValidatePaymentAddress(ctx, tc.addr)
			switch {
			case err != nil && tc.good:
				t.Fatalf("unexpected failure: %v", err)
			case err == nil && !tc.good:
				t.Fatalf("unexpected success: wanted error")
			}
		})
	}
}
