package storetest

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"zenith/cryptoutil"
	"zenith/store"

	sdk_types_bech32 "github.com/cosmos/cosmos-sdk/types/bech32"
	tm_crypto_secp256k1 "github.com/tendermint/tendermint/crypto/secp256k1"
)

const (
	ChainID              = "test-chain-id"
	Denom                = "uzen"
	Network              = "zenith"
	MekatekPaymentAddr   = "zenith1kwwvsp08xyd9saq84cz4mdl0kyf58hwfzhe9hd"
	ValidatorPaymentAddr = "zenith1srcq5ngt87ryg2s6zmpr39knpx24jv3y0ud24n"
)

func NewChain(t *testing.T, s store.Store) *store.Chain {
	t.Helper()

	pubKey := tm_crypto_secp256k1.GenPrivKey().PubKey()
	addr := GetBech32Addr(t, Network, pubKey.Address().Bytes())

	c := &store.Chain{
		ID:                    fmt.Sprintf("%s-%d", Network, rand.Int()),
		Network:               Network,
		MekatekPaymentAddress: addr,
		PaymentDenom:          Denom,
		Timeout:               time.Second,
		NodeURIs:              []string{"http://foo:4566/", "https://bar:4567/baz"},
	}

	err := s.UpsertChain(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}

	return c
}

func NewChallenge(t *testing.T, s store.Store, c *store.Chain) *store.Challenge {
	t.Helper()

	pubKey := tm_crypto_secp256k1.GenPrivKey().PubKey()
	addr := GetBech32Addr(t, c.Network, pubKey.Address().Bytes())

	ch := &store.Challenge{
		ChainID:          c.ID,
		ValidatorAddress: addr,
		PubKeyBytes:      pubKey.Bytes(),
		PubKeyType:       pubKey.Type(),
		PaymentAddress:   addr,
		Challenge:        cryptoutil.RandomBytes(32),
	}

	if err := s.InsertChallenge(context.Background(), ch); err != nil {
		t.Fatal(err)
	}

	return ch
}

func NewValidator(t *testing.T, s store.Store, c *store.Chain) *store.Validator {
	t.Helper()

	pubKey := tm_crypto_secp256k1.GenPrivKey().PubKey()
	addr := GetBech32Addr(t, c.Network, pubKey.Address().Bytes())
	moniker := getFunName(t)

	v := &store.Validator{
		ChainID:        c.ID,
		Address:        addr,
		Moniker:        moniker,
		PubKeyBytes:    pubKey.Bytes(),
		PubKeyType:     pubKey.Type(),
		PaymentAddress: addr,
	}

	err := s.UpsertValidator(context.Background(), v)
	if err != nil {
		t.Fatal(err)
	}

	return v
}

func NewAuction(t *testing.T, s store.Store, c *store.Chain, height int64, v *store.Validator) *store.Auction {
	t.Helper()

	a := &store.Auction{
		ChainID:                 c.ID,
		Height:                  height,
		ValidatorAddress:        v.Address,
		ValidatorAllocation:     0.9,
		ValidatorPaymentAddress: v.PaymentAddress,
		MekatekPaymentAddress:   c.MekatekPaymentAddress,
		PaymentDenom:            c.PaymentDenom,
		RegisteredPower:         100,
		TotalPower:              1000,
	}

	err := s.UpsertAuction(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}

	return a
}

func NewBid(t *testing.T, s store.Store, c *store.Chain, a *store.Auction) *store.Bid {
	t.Helper()

	addr := GenBech32Addr(t, Network)
	b := &store.Bid{
		ChainID:          c.ID,
		Height:           a.Height,
		Kind:             store.BidKindTop,
		Txs:              [][]byte{{0x01}, {0x02}},
		MekatekPayment:   100,
		ValidatorPayment: 900,
		Priority:         1,
		State:            store.BidStatePending,
		Payments: []store.Payment{
			{From: addr, To: a.MekatekPaymentAddress, Amount: 100},
			{From: addr, To: a.ValidatorPaymentAddress, Amount: 900},
		},
	}

	err := s.InsertBid(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}

	return b
}

func GetBech32Addr(t *testing.T, prefix string, addr []byte) string {
	bech32Addr, err := sdk_types_bech32.ConvertAndEncode(prefix, addr)
	if err != nil {
		t.Fatal(err)
	}
	return bech32Addr
}

func GetBech32AddrString(t *testing.T, prefix string, s string) string {
	addr, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex.DecodeString: %v", err)
	}
	return GetBech32Addr(t, prefix, addr)
}

func GenBech32Addr(t *testing.T, prefix string) string {
	return GetBech32Addr(t, prefix, tm_crypto_secp256k1.GenPrivKey().PubKey().Address())
}

func getFunName(t *testing.T) string {
	t.Helper()

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	buf := make([]byte, 8)
	if _, err := rng.Read(buf); err != nil {
		t.Fatal("randomness is invalid")
	}

	return fmt.Sprintf("moniker-%x", buf)
}
