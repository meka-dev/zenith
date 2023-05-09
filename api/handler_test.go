package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"zenith/api"
	"zenith/block"
	"zenith/chain"
	"zenith/store/memstore"
	"zenith/store/storetest"

	"github.com/go-kit/log"
	"github.com/meka-dev/mekatek-go/mekabuild"
	tm_crypto_secp256k1 "github.com/tendermint/tendermint/crypto/secp256k1"
)

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

func TestGetAuctionContentTypes(t *testing.T) {
	// TODO: copied from block test, extract to its own set of test helpers

	var (
		ctx          = context.Background()
		foo          = newTestValidator()
		bar          = newTestValidator()
		baz          = newTestValidator()
		height       = int64(123)
		validatorSet = chain.ValidatorSet{
			Height: height,
			Set: map[string]*chain.Validator{
				foo.Address: foo.Validator,
				bar.Address: bar.Validator,
				baz.Address: baz.Validator,
			},
			TotalPower: foo.VotingPower + bar.VotingPower + baz.VotingPower,
		}
		proposer    = bar
		paymentAddr = storetest.GetBech32AddrString(t, storetest.Network, proposer.Address)
		testStore   = memstore.NewStore()
		storeChain  = storetest.NewChain(t, testStore)
		mockChain   = &chain.TestChain{ChainID: storeChain.ID, Height: height, Validators: validatorSet, PredictedProposer: *bar.Validator}
		service     = block.NewCoreService(mockChain, testStore)
		manager     = block.NewStaticServiceManager(service)
		logger      = log.NewLogfmtLogger(os.Stderr)
		handler     = api.NewHandler(testStore, manager, logger)
	)

	{
		challenge, err := service.Apply(ctx, proposer.Address, paymentAddr)
		if err != nil {
			t.Fatalf("initial register: %v", err)
		}
		msg := mekabuild.RegisterChallengeSignBytes(challenge.ChainID, challenge.Challenge)
		signature, err := proposer.sign(msg)
		if err != nil {
			t.Fatalf("sign challenge: %v", err)
		}
		v, err := service.Register(ctx, challenge.ID.String(), signature)
		if err != nil {
			t.Fatalf("second register: %v", err)
		}
		if want, have := paymentAddr, v.PaymentAddress; want != have {
			t.Fatalf("payment addr: want %s, have %s", want, have)
		}
	}

	server := httptest.NewServer(handler)
	t.Cleanup(func() { server.Close() })

	check := func(t *testing.T, resp *http.Response, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		var auction struct {
			ChainID  string `json:"chain_id"`
			Height   int64  `json:"height"`
			Payments []struct {
				Address string `json:"address"`
			} `json:"payments"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&auction); err != nil {
			t.Fatal(err)
		}
		if want, have := storeChain.ID, auction.ChainID; want != have {
			t.Errorf("chain ID: want %q, have %q", want, have)
		}
		if want, have := height, auction.Height; want != have {
			t.Errorf("height: want %d, have %d", want, have)
		}
		if len(auction.Payments) <= 0 {
			t.Fatalf("payments empty")
		}
		if want, have := paymentAddr, auction.Payments[0].Address; want != have {
			t.Errorf("first payment address: want %q, have %q", want, have)
		}
	}

	t.Run("JSON with no content type", func(t *testing.T) {
		body := fmt.Sprintf(`{"chain_id": "%s", "height": %d}`, storeChain.ID, height)
		req, _ := http.NewRequest("GET", server.URL+"/v0/auction", strings.NewReader(body))
		resp, err := http.DefaultClient.Do(req)
		check(t, resp, err)
	})

	t.Run("application/json", func(t *testing.T) {
		body := fmt.Sprintf(`{"chain_id": "%s", "height": %d}`, storeChain.ID, height)
		req, _ := http.NewRequest("GET", server.URL+"/v0/auction", strings.NewReader(body))
		req.Header.Set("content-type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		check(t, resp, err)
	})

	t.Run("URL", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf(server.URL+`/v0/auction?chain_id=%s&height=%d`, storeChain.ID, height))
		check(t, resp, err)
	})

	t.Run("application/x-www-form-urlencoded", func(t *testing.T) {
		data := url.Values{}
		data.Set("chain_id", storeChain.ID)
		data.Set("height", strconv.FormatInt(height, 10))
		encoded := data.Encode()
		body := strings.NewReader(encoded)
		req, _ := http.NewRequest("GET", server.URL+`/v0/auction`, body)
		req.Header.Set("content-type", "application/x-www-form-urlencoded")
		resp, err := http.DefaultClient.Do(req)
		check(t, resp, err)
	})
}

func TestHandlerGzip(t *testing.T) {
	var (
		ctx      = context.Background()
		myErr    = errors.New("sigil error from service layer")
		store    = memstore.NewStore()
		chainID  = "my-chain"
		service  = block.NewMockServiceErr(chainID, myErr)
		services = block.NewStaticServiceManager(service)
		logger   = log.NewNopLogger()
		handler  = api.NewHandler(store, services, logger)
	)

	server := httptest.NewServer(handler)
	t.Cleanup(func() { server.Close() })

	var (
		cli           = &http.Client{}
		apiURL, _     = url.Parse(server.URL)
		signer        = &mockSigner{Error: errors.New("no sign for you")}
		validatorAddr = "validator"
		paymentAddr   = "payment"
		builder       = mekabuild.NewBuilder(cli, apiURL, signer, chainID, validatorAddr, paymentAddr)
	)

	for _, compression := range []bool{true, false} {
		t.Run(fmt.Sprintf("compression=%v", compression), func(t *testing.T) {
			builder.SetCompression(compression)
			want := myErr
			have := builder.Register(ctx)
			pass := strings.Contains(have.Error(), want.Error())
			switch {
			case pass:
				t.Logf("got (expected) error: %v", have)
			case !pass:
				t.Errorf("want %v, have %v", want, have)
			}
		})
	}
}

//
//
//

type mockSigner struct{ Error error }

func (m *mockSigner) SignBuildBlockRequest(*mekabuild.BuildBlockRequest) error {
	return m.Error
}

func (m *mockSigner) SignRegisterChallenge(*mekabuild.RegisterChallenge) error {
	return m.Error
}
