package zcosmos

import (
	"context"
	"math"
	"net/http"
	"testing"
	"time"
	"zenith/chain"

	"github.com/sebdah/goldie/v2"
	"gopkg.in/dnaeon/go-vcr.v3/recorder"
)

func ChainValidatorSetTest(t *testing.T, c *Chain, fixtureDir string) {
	t.Helper()

	ctx := context.WithValue(context.Background(), sequentialKey, true) // TODO: hack, fix

	{
		// TODO: hack, fix
		cc := *c
		cc.stallThreshold = time.Duration(math.MaxInt64)
		c = &cc
	}

	latestHeight, err := c.LatestHeight(ctx)
	if err != nil {
		t.Fatalf("latest height: %v", err)
	}

	t.Logf("'latest' height %d", latestHeight)

	valset, err := c.ValidatorSet(ctx, latestHeight)
	if err != nil {
		t.Fatalf("get validator set: %v", err)
	}

	t.Logf("validator set height %d, size %d, total power %d", valset.Height, len(valset.Validators), valset.TotalPower)

	type heightProposer struct {
		Height   int64
		Proposer *chain.Validator
	}

	var have []heightProposer
	for _, offset := range []int64{1, 3, 6, 9} {
		height := latestHeight + offset

		p, err := c.PredictProposer(ctx, valset, height)
		if err != nil {
			t.Fatal(err)
		}

		have = append(have, heightProposer{height, p})
	}

	goldie.New(t, goldie.WithFixtureDir(fixtureDir)).AssertJson(t, t.Name(), have)
}

func RecorderClient(t *testing.T) *http.Client {
	t.Helper()

	rec, err := recorder.NewWithOptions(&recorder.Options{
		CassetteName:       "testdata/vcr/" + t.Name(),
		Mode:               recorder.ModeRecordOnce,
		SkipRequestLatency: true,
		RealTransport:      http.DefaultTransport,
	})
	if err != nil {
		t.Fatal(err)
	}

	switch rec.IsNewCassette() {
	case true:
		t.Logf("testdata missing: will query RPC endpoint")
	case false:
		t.Logf("testdata exists: won't query RPC endpoint")
	}

	t.Cleanup(func() {
		if err := rec.Stop(); err != nil {
			t.Fatal(err)
		}
	})

	return rec.GetDefaultClient()
}
