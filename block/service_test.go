package block_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"zenith/block"
	"zenith/chain"
	"zenith/store"
	"zenith/store/memstore"
	"zenith/store/pgstore"
	"zenith/store/storetest"

	"github.com/meka-dev/mekatek-go/mekabuild"
	tm_crypto_secp256k1 "github.com/tendermint/tendermint/crypto/secp256k1"
)

func newStore(t *testing.T, ctx context.Context) store.Store {
	switch {
	case os.Getenv("PGCONNSTRING") != "":
		t.Logf("using Postgres store")
		return pgstore.NewTestStore(t)
	default:
		t.Logf("using memory store (set PGCONNSTR to use Postgres)")
		return memstore.NewStore()
	}
}

func TestServiceRegister(t *testing.T) {
	t.Parallel()

	var (
		ctx    = context.Background()
		foo    = newTestValidator()
		bar    = newTestValidator()
		baz    = newTestValidator()
		height = int64(123)
		valset = chain.ValidatorSet{
			Height: height,
			Set: map[string]*chain.Validator{
				foo.Address: foo.Validator,
				bar.Address: bar.Validator,
				baz.Address: baz.Validator,
			},
			TotalPower: foo.VotingPower + bar.VotingPower + baz.VotingPower,
		}
	)

	t.Run("register-and-update", func(t *testing.T) {
		var (
			testStore          = newStore(t, ctx)
			storeChain         = storetest.NewChain(t, testStore)
			mockChain          = &chain.TestChain{ChainID: storeChain.ID, Height: height, Validators: valset, PredictedProposer: *bar.Validator}
			service            = block.NewCoreService(mockChain, testStore)
			validator          = bar
			initalPaymentAddr  = storetest.GetBech32AddrString(t, storetest.Network, validator.Address)
			updatedPaymentAddr = storetest.GenBech32Addr(t, storetest.Network)
		)

		for _, paymentAddr := range []string{initalPaymentAddr, updatedPaymentAddr} {
			t.Logf("validator %s, payment %s", validator.Address, paymentAddr)

			challenge, err := service.Apply(ctx, validator.Address, paymentAddr)
			if err != nil {
				t.Fatalf("initial register: %v", err)
			}

			t.Logf("Apply: challenge ID %s", challenge.ID.String())

			msg := mekabuild.RegisterChallengeSignBytes(challenge.ChainID, challenge.Challenge)
			signature, err := validator.sign(msg)
			if err != nil {
				t.Fatalf("sign challenge: %v", err)
			}

			v, err := service.Register(ctx, challenge.ID.String(), signature)
			if err != nil {
				t.Fatalf("second register: %v", err)
			}

			t.Logf("Register: returned payment addr %s", v.PaymentAddress)

			if want, have := paymentAddr, v.PaymentAddress; want != have {
				t.Fatalf("payment addr: want %s, have %s", want, have)
			}
		}
	})
}

func TestServiceAuction(t *testing.T) {
	t.Parallel()

	var (
		ctx    = context.Background()
		foo    = newTestValidator()
		bar    = newTestValidator()
		baz    = newTestValidator()
		height = int64(123)
		valset = chain.ValidatorSet{
			Height: height,
			Set: map[string]*chain.Validator{
				foo.Address: foo.Validator,
				bar.Address: bar.Validator,
				baz.Address: baz.Validator,
			},
			TotalPower: foo.VotingPower + bar.VotingPower + baz.VotingPower,
		}
	)

	t.Run("auction too far in the past", func(t *testing.T) {
		var (
			testStore  = newStore(t, ctx)
			storeChain = storetest.NewChain(t, testStore)
			mockChain  = &chain.TestChain{ChainID: storeChain.ID, Height: height, Validators: valset, PredictedProposer: *bar.Validator}
			service    = block.NewCoreService(mockChain, testStore)
		)

		_, err := service.Auction(ctx, height-3)
		if want, have := block.ErrAuctionTooOld, err; !errors.Is(have, want) {
			t.Fatalf("want %v, have %v", want, have)
		}
	})

	t.Run("auction too far in the future", func(t *testing.T) {
		var (
			testStore  = newStore(t, ctx)
			storeChain = storetest.NewChain(t, testStore)
			mockChain  = &chain.TestChain{ChainID: storeChain.ID, Height: height, Validators: valset, PredictedProposer: *bar.Validator}
			service    = block.NewCoreService(mockChain, testStore)
		)

		_, err := service.Auction(ctx, height+25)
		if want, have := block.ErrAuctionTooNew, err; !errors.Is(have, want) {
			t.Fatalf("want %v, have %v", want, have)
		}
	})

	t.Run("auction unavailable", func(t *testing.T) {
		var (
			testStore  = newStore(t, ctx)
			storeChain = storetest.NewChain(t, testStore)
			mockChain  = &chain.TestChain{ChainID: storeChain.ID, Height: height, Validators: valset, PredictedProposer: *bar.Validator}
			service    = block.NewCoreService(mockChain, testStore)
		)

		_, err := service.Auction(ctx, height+1)
		if want, have := block.ErrAuctionUnavailable, err; !errors.Is(have, want) {
			t.Fatalf("want %v, have %v", want, have)
		}
	})

	t.Run("auction already finished", func(t *testing.T) {
		var (
			testStore  = newStore(t, ctx)
			storeChain = storetest.NewChain(t, testStore)
			mockChain  = &chain.TestChain{ChainID: storeChain.ID, Height: height, Validators: valset, PredictedProposer: *bar.Validator}
			service    = block.NewCoreService(mockChain, testStore)
		)

		for _, v := range mockChain.Validators.Set {
			err := testStore.UpsertValidator(ctx, &block.Validator{
				ChainID:        storeChain.ID,
				Address:        v.Address,
				PubKeyBytes:    v.PubKeyBytes,
				PubKeyType:     v.PubKeyType,
				PaymentAddress: v.Address,
			})
			if err != nil {
				t.Fatalf("register val: %v", err)
			}
		}

		auction, err := service.Auction(ctx, height+1)
		if err != nil {
			t.Fatalf("auction: %v", err)
		}

		auction.FinishedAt = time.Now().UTC()
		if err := testStore.UpsertAuction(ctx, auction); err != nil {
			t.Fatalf("finish auction: %v", err)
		}

		_, err = service.Auction(ctx, height+1)
		if want, have := block.ErrAuctionFinished, err; !errors.Is(have, want) {
			t.Fatalf("want %v, have %v", want, have)
		}
	})
}

func TestServiceBid(t *testing.T) {
	t.Parallel()

	var (
		ctx    = context.Background()
		foo    = newTestValidator()
		bar    = newTestValidator()
		baz    = newTestValidator()
		height = int64(123)
		valset = chain.ValidatorSet{
			Height: height,
			Set: map[string]*chain.Validator{
				foo.Address: foo.Validator,
				bar.Address: bar.Validator,
				baz.Address: baz.Validator,
			},
			TotalPower: foo.VotingPower + bar.VotingPower + baz.VotingPower,
		}
	)

	t.Run("auction too far in the past", func(t *testing.T) {
		var (
			testStore  = newStore(t, ctx)
			storeChain = storetest.NewChain(t, testStore)
			mockChain  = &chain.TestChain{ChainID: storeChain.ID, Height: height, Validators: valset, PredictedProposer: *bar.Validator}
			service    = block.NewCoreService(mockChain, testStore)
		)

		_, err := service.Bid(ctx, height-3, string(store.BidKindTop), [][]byte{{0, 1, 2}})
		if want, have := block.ErrAuctionTooOld, err; !errors.Is(have, want) {
			t.Fatalf("want %v, have %v", want, have)
		}
	})

	t.Run("auction too far in the future", func(t *testing.T) {
		var (
			testStore  = newStore(t, ctx)
			storeChain = storetest.NewChain(t, testStore)
			mockChain  = &chain.TestChain{ChainID: storeChain.ID, Height: height, Validators: valset, PredictedProposer: *bar.Validator}
			service    = block.NewCoreService(mockChain, testStore)
		)

		_, err := service.Bid(ctx, height+25, string(store.BidKindTop), [][]byte{{0, 1, 2}})
		if want, have := block.ErrAuctionTooNew, err; !errors.Is(have, want) {
			t.Fatalf("want %v, have %v", want, have)
		}
	})

	t.Run("auction unavailable", func(t *testing.T) {
		var (
			testStore  = newStore(t, ctx)
			storeChain = storetest.NewChain(t, testStore)
			mockChain  = &chain.TestChain{ChainID: storeChain.ID, Height: height, Validators: valset, PredictedProposer: *bar.Validator}
			service    = block.NewCoreService(mockChain, testStore)
		)

		_, err := service.Bid(ctx, height+1, string(store.BidKindTop), [][]byte{{0, 1, 2}})
		if want, have := block.ErrAuctionUnavailable, err; !errors.Is(have, want) {
			t.Fatalf("want %v, have %v", want, have)
		}
	})

	t.Run("auction already finished", func(t *testing.T) {
		var (
			testStore  = newStore(t, ctx)
			storeChain = storetest.NewChain(t, testStore)
			mockChain  = &chain.TestChain{ChainID: storeChain.ID, Height: height, Validators: valset, PredictedProposer: *bar.Validator}
			service    = block.NewCoreService(mockChain, testStore)
		)

		for _, v := range mockChain.Validators.Set {
			err := testStore.UpsertValidator(ctx, &block.Validator{
				ChainID:        storeChain.ID,
				Address:        v.Address,
				PubKeyBytes:    v.PubKeyBytes,
				PubKeyType:     v.PubKeyType,
				PaymentAddress: v.Address,
			})
			if err != nil {
				t.Fatalf("register val: %v", err)
			}
		}

		auction, err := service.Auction(ctx, height+1)
		if err != nil {
			t.Fatalf("auction: %v", err)
		}

		auction.FinishedAt = time.Now().UTC()
		if err := testStore.UpsertAuction(ctx, auction); err != nil {
			t.Fatalf("finish auction: %v", err)
		}

		_, err = service.Bid(ctx, height+1, string(store.BidKindTop), [][]byte{{0, 1, 2}})
		if want, have := block.ErrAuctionFinished, err; !errors.Is(have, want) {
			t.Fatalf("want %v, have %v", want, have)
		}
	})
}

func TestServiceBuild(t *testing.T) {
	t.Skip("TODO")
}

func TestAllocation(t *testing.T) {
	for _, tc := range []struct {
		registered int64
		total      int64
		want       float64
	}{
		{0, 100, 0.5},
		{1, 100, 0.504},
		{20, 100, 0.58},
		{50, 100, 0.7},
		{67, 100, 0.768},
		{100, 100, 0.9},
		{99999, 100000, 0.9},
	} {
		tc := tc
		t.Run(fmt.Sprintf("%d:%d", tc.registered, tc.total), func(t *testing.T) {
			want := tc.want
			have := block.Allocation(tc.registered, tc.total)
			if !floatEqual(want, have, 0.01) {
				t.Fatalf("want %f, have %f", want, have)
			}
		})
	}
}

//
//
//

func floatEqual(a, b, tolerance float64) bool {
	return math.Abs(a-b) < tolerance
}

type testValidator struct {
	*chain.Validator
	sign func([]byte) ([]byte, error)
}

func newTestValidator() *testValidator {
	var (
		privKey     = tm_crypto_secp256k1.GenPrivKey()
		pubKey      = privKey.PubKey()
		votingPower = int64(1.0)
	)
	return &testValidator{
		Validator: &chain.Validator{
			Address:     pubKey.Address().String(),
			PubKeyType:  pubKey.Type(),
			PubKeyBytes: pubKey.Bytes(),
			VotingPower: votingPower,
		},
		sign: privKey.Sign,
	}
}
