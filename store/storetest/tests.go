package storetest

import (
	"context"
	"errors"
	"sort"
	"testing"

	"zenith/store"

	"github.com/gofrs/uuid"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestStore(t *testing.T, makeStore func(*testing.T) store.Store) {
	ctx := context.Background()

	t.Run("ListBids", func(t *testing.T) {
		s := makeStore(t)
		chain := NewChain(t, s)
		validator := NewValidator(t, s, chain)
		auction := NewAuction(t, s, chain, 1, validator)
		bid1 := NewBid(t, s, chain, auction)
		bid2 := NewBid(t, s, chain, auction)

		bids, err := s.ListBids(ctx, chain.ID, auction.Height)
		if err != nil {
			t.Fatal(err)
		}

		want := []*store.Bid{bid1, bid2}
		if diff := cmp.Diff(bids, want); diff != "" {
			t.Fatalf("mismatch: %s", diff)
		}
	})

	t.Run("UpdateBids", func(t *testing.T) {
		s := makeStore(t)
		chain := NewChain(t, s)
		validator := NewValidator(t, s, chain)
		auction := NewAuction(t, s, chain, 1, validator)
		bid1 := NewBid(t, s, chain, auction)
		bid2 := NewBid(t, s, chain, auction)

		bid1.State = store.BidStateRejected
		bid2.State = store.BidStateAccepted

		err := s.UpdateBids(ctx, bid1, bid2)
		if err != nil {
			t.Fatal(err)
		}

		bids, err := s.ListBids(ctx, chain.ID, auction.Height)
		if err != nil {
			t.Fatal(err)
		}

		ignore := cmpopts.IgnoreFields(store.Bid{}, "UpdatedAt")
		want := []*store.Bid{bid1, bid2}
		if diff := cmp.Diff(bids, want, ignore); diff != "" {
			t.Fatalf("mismatch: %s", diff)
		}
	})

	t.Run("SelectAuction", func(t *testing.T) {
		s := makeStore(t)
		chain := NewChain(t, s)
		validator := NewValidator(t, s, chain)
		auction := NewAuction(t, s, chain, 1, validator)

		have, err := s.SelectAuction(ctx, chain.ID, auction.Height)
		if err != nil {
			t.Fatal(err)
		}

		want := auction
		if diff := cmp.Diff(have, want); diff != "" {
			t.Fatalf("mismatch: %s", diff)
		}
	})

	t.Run("SelectChallenge", func(t *testing.T) {
		s := makeStore(t)
		chain := NewChain(t, s)
		challenge := NewChallenge(t, s, chain)

		have, err := s.SelectChallenge(ctx, challenge.ID.String())
		if err != nil {
			t.Fatal(err)
		}

		want := challenge
		if diff := cmp.Diff(have, want); diff != "" {
			t.Fatalf("mismatch: %s", diff)
		}
	})

	t.Run("DeleteChallenge", func(t *testing.T) {
		s := makeStore(t)
		chain := NewChain(t, s)

		bogusUUID, _ := uuid.NewV4()
		want := store.ErrNotFound
		have := s.DeleteChallenge(ctx, bogusUUID.String())
		if !errors.Is(have, want) {
			t.Fatalf("delete bogus challenge: want %v, have %v", want, have)
		}

		challenge := NewChallenge(t, s, chain)
		if err := s.DeleteChallenge(ctx, challenge.ID.String()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("SelectValidator", func(t *testing.T) {
		s := makeStore(t)
		chain := NewChain(t, s)
		validator := NewValidator(t, s, chain)

		want := validator
		have, err := s.SelectValidator(ctx, chain.ID, validator.Address)
		if err != nil {
			t.Fatal(err)
		}

		if diff := cmp.Diff(have, want); diff != "" {
			t.Fatalf("mismatch: %s", diff)
		}
	})

	t.Run("ListValidators", func(t *testing.T) {
		s := makeStore(t)
		chain := NewChain(t, s)

		want := []*store.Validator(nil)
		have, err := s.ListValidators(ctx, chain.ID)
		if err != nil {
			t.Fatal(err)
		}

		if diff := cmp.Diff(have, want); diff != "" {
			t.Fatalf("mismatch: %s", diff)
		}

		want = []*store.Validator{
			NewValidator(t, s, chain),
			NewValidator(t, s, chain),
		}

		sort.SliceStable(want, func(i, j int) bool {
			return want[i].Address < want[j].Address
		})

		have, err = s.ListValidators(ctx, chain.ID)
		if err != nil {
			t.Fatal(err)
		}

		if diff := cmp.Diff(have, want); diff != "" {
			t.Fatalf("mismatch: %s", diff)
		}
	})

	t.Run("SelectChain", func(t *testing.T) {
		s := makeStore(t)
		chain := NewChain(t, s)

		want := chain
		have, err := s.SelectChain(ctx, chain.ID)
		if err != nil {
			t.Fatal(err)
		}

		ignore := cmpopts.IgnoreFields(store.Chain{}, "CreatedAt", "UpdatedAt")
		if diff := cmp.Diff(have, want, ignore); diff != "" {
			t.Fatalf("mismatch: %s", diff)
		}
	})

	t.Run("ListChains", func(t *testing.T) {
		s := makeStore(t)
		chain1 := NewChain(t, s)
		chain2 := NewChain(t, s)

		want := []*store.Chain{chain1, chain2}
		sort.SliceStable(want, func(i, j int) bool {
			return want[i].ID < want[j].ID
		})

		have, err := s.ListChains(ctx)
		if err != nil {
			t.Fatal(err)
		}

		ignore := cmpopts.IgnoreFields(store.Chain{}, "CreatedAt", "UpdatedAt")
		if diff := cmp.Diff(have, want, ignore); diff != "" {
			t.Fatalf("mismatch: %s", diff)
		}
	})
}
