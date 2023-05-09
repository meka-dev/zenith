package main

import (
	"os"
	"path/filepath"
	"testing"
	"zcosmos"
)

func TestChain(t *testing.T) {
	nodeURI := os.Getenv("TEST_NODE_URI")
	if nodeURI == "" {
		nodeURI = "http://fra-stockosm-1:26657"
	}

	var (
		chainID       = "osmosis-1"
		rpcAddrs      = []string{nodeURI}
		httpClient    = zcosmos.RecorderClient(t)
		fixtureDir, _ = filepath.Abs("testdata/golden")
	)
	chain, err := zcosmos.NewChain(networkConfig, chainID, rpcAddrs, httpClient)
	if err != nil {
		t.Fatal(err)
	}

	zcosmos.ChainValidatorSetTest(t, chain, fixtureDir)
}
