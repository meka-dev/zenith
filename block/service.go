package block

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"mekapi/trc"
	"mekapi/trc/eztrc"
	"sort"
	"time"

	"zenith/chain"
	"zenith/cryptoutil"
	"zenith/metrics"
	"zenith/store"

	"github.com/meka-dev/mekatek-go/mekabuild"
)

// These type aliases are quick and hacky way to ensure that the API of `package
// block` doesn't include types defined in `package store`.
type (
	Chain     = store.Chain
	Auction   = store.Auction
	Bid       = store.Bid
	Payment   = store.Payment
	Challenge = store.Challenge
	Validator = store.Validator
)

type Service interface {
	ChainID() string
	Ping(ctx context.Context) error
	Auction(ctx context.Context, height int64) (*Auction, error)
	Bid(ctx context.Context, height int64, kind string, txs [][]byte) (*Bid, error)
	Apply(ctx context.Context, validatorAddr string, paymentAddr string) (*Challenge, error)
	Register(ctx context.Context, challengeID string, signature []byte) (*Validator, error)
	Build(ctx context.Context, height int64, validatorAddr string, maxBytes, maxGas int64, txs [][]byte, signature []byte) ([][]byte, string, error)
	BuildV1(ctx context.Context, height int64, validatorAddr string, maxBytes, maxGas int64, txs [][]byte, signature []byte) ([][]byte, string, error)
}

//
//
//

type MockService struct {
	ChainIDFunc  func() string
	PingFunc     func(ctx context.Context) error
	AuctionFunc  func(ctx context.Context, height int64) (*Auction, error)
	BidFunc      func(ctx context.Context, height int64, kind string, txs [][]byte) (*Bid, error)
	ApplyFunc    func(ctx context.Context, validatorAddr string, paymentAddr string) (*Challenge, error)
	RegisterFunc func(ctx context.Context, challengeID string, signature []byte) (*Validator, error)
	BuildFunc    func(ctx context.Context, height int64, validatorAddr string, maxBytes, maxGas int64, txs [][]byte, signature []byte) ([][]byte, string, error)
	BuildV1Func  func(ctx context.Context, height int64, validatorAddr string, maxBytes, maxGas int64, txs [][]byte, signature []byte) ([][]byte, string, error)
}

func NewMockServiceErr(chainID string, err error) *MockService {
	return &MockService{
		ChainIDFunc: func() string {
			return chainID
		},
		PingFunc: func(ctx context.Context) error {
			return err
		},
		AuctionFunc: func(ctx context.Context, height int64) (*Auction, error) {
			return nil, err
		},
		BidFunc: func(ctx context.Context, height int64, kind string, txs [][]byte) (*Bid, error) {
			return nil, err
		},
		ApplyFunc: func(ctx context.Context, validatorAddr string, paymentAddr string) (*Challenge, error) {
			return nil, err
		},
		RegisterFunc: func(ctx context.Context, challengeID string, signature []byte) (*Validator, error) {
			return nil, err
		},
		BuildFunc: func(ctx context.Context, height int64, validatorAddr string, maxBytes, maxGas int64, txs [][]byte, signature []byte) ([][]byte, string, error) {
			return nil, "", err
		},
		BuildV1Func: func(ctx context.Context, height int64, validatorAddr string, maxBytes, maxGas int64, txs [][]byte, signature []byte) ([][]byte, string, error) {
			return nil, "", err
		},
	}
}

func (m *MockService) ChainID() string {
	return m.ChainIDFunc()
}

func (m *MockService) Ping(ctx context.Context) error {
	return m.PingFunc(ctx)
}

func (m *MockService) Auction(ctx context.Context, height int64) (*Auction, error) {
	return m.AuctionFunc(ctx, height)
}

func (m *MockService) Bid(ctx context.Context, height int64, kind string, txs [][]byte) (*Bid, error) {
	return m.BidFunc(ctx, height, kind, txs)
}

func (m *MockService) Apply(ctx context.Context, validatorAddr string, paymentAddr string) (*Challenge, error) {
	return m.ApplyFunc(ctx, validatorAddr, paymentAddr)
}

func (m *MockService) Register(ctx context.Context, challengeID string, signature []byte) (*Validator, error) {
	return m.RegisterFunc(ctx, challengeID, signature)
}

func (m *MockService) Build(ctx context.Context, height int64, validatorAddr string, maxBytes, maxGas int64, txs [][]byte, signature []byte) ([][]byte, string, error) {
	return m.BuildFunc(ctx, height, validatorAddr, maxBytes, maxGas, txs, signature)
}

func (m *MockService) BuildV1(ctx context.Context, height int64, validatorAddr string, maxBytes, maxGas int64, txs [][]byte, signature []byte) ([][]byte, string, error) {
	return m.BuildV1Func(ctx, height, validatorAddr, maxBytes, maxGas, txs, signature)
}

//
//
//

type CoreService struct {
	chain chain.Chain
	store store.Store
}

var _ Service = (*CoreService)(nil)

func NewCoreService(c chain.Chain, s store.Store) *CoreService {
	return &CoreService{
		chain: c,
		store: s,
	}
}

func (s *CoreService) ChainID() string {
	return s.chain.ID()
}

func (s *CoreService) Ping(ctx context.Context) error {
	ctx = trc.PrefixContextf(ctx, "[Ping]")

	if err := s.store.Ping(ctx); err != nil {
		return fmt.Errorf("ping store: %w", err)
	}

	return nil
}

func (s *CoreService) Auction(ctx context.Context, height int64) (_ *Auction, err error) {
	ctx = trc.PrefixContextf(ctx, "[Auction]")

	defer func() {
		result := boolString(err == nil, "success", "error")
		metrics.AuctionRequestsTotal.WithLabelValues(s.chain.ID(), result).Inc()
	}()

	eztrc.Tracef(ctx, "requested auction height %d", height)

	var auction *Auction
	{
		if err := s.store.Transact(ctx, func(tx store.Store) error {
			a, _, err := verifyAuction(ctx, s.chain, height, 10, tx)
			if err != nil {
				return err
			}
			auction = a
			return nil
		}); err != nil {
			return nil, err
		}
	}

	return auction, nil
}

func (s *CoreService) Bid(ctx context.Context, height int64, kind string, txs [][]byte) (_ *Bid, err error) {
	ctx = trc.PrefixContextf(ctx, "[Bid]")

	eztrc.Tracef(ctx, "height %d", height)
	eztrc.Tracef(ctx, "kind %s", kind)
	eztrc.Tracef(ctx, "tx count %d", len(txs))

	defer func() {
		switch {
		case err == nil:
			metrics.BidsSubmittedTotal.WithLabelValues(s.chain.ID(), "success").Inc()
			metrics.BidTxsSubmittedTotal.WithLabelValues(s.chain.ID(), "success").Add(float64(len(txs)))
		case err != nil:
			metrics.BidsSubmittedTotal.WithLabelValues(s.chain.ID(), "error").Inc()
			metrics.BidTxsSubmittedTotal.WithLabelValues(s.chain.ID(), "error").Add(float64(len(txs)))
		}
	}()

	auction, err := s.Auction(ctx, height)
	if err != nil {
		return nil, fmt.Errorf("fetch auction: %w", err)
	}

	bid := &Bid{
		ChainID: auction.ChainID,
		Height:  auction.Height,
		Kind:    store.ParseBidKind(kind),
		State:   store.BidStatePending,
		Txs:     txs,
	}

	if err := evaluateBid(ctx, s.chain, auction, bid); err != nil {
		return nil, fmt.Errorf("evaluate bid: %w", err)
	}

	if err := s.store.InsertBid(ctx, bid); err != nil {
		return nil, fmt.Errorf("place bid: %w", err)
	}

	return bid, nil
}

func (s *CoreService) Apply(ctx context.Context, validatorAddr string, paymentAddr string) (_ *Challenge, err error) {
	ctx = trc.PrefixContextf(ctx, "[Apply]")

	defer func() {
		switch {
		case err == nil:
			metrics.RegisterRequestsTotal.WithLabelValues(s.chain.ID(), "apply", "challenged").Inc()
		case err != nil:
			metrics.RegisterRequestsTotal.WithLabelValues(s.chain.ID(), "apply", "error").Inc()
		}
	}()

	eztrc.Tracef(ctx, "validator addr %s", validatorAddr)
	eztrc.Tracef(ctx, "payment addr %s", paymentAddr)

	if err := s.chain.ValidatePaymentAddress(ctx, paymentAddr); err != nil {
		return nil, fmt.Errorf("payment address (%s): %w", paymentAddr, err)
	}

	latestHeight, err := s.chain.LatestHeight(ctx)
	if err != nil {
		return nil, fmt.Errorf("get latest height: %w", err)
	}

	vs, err := s.chain.ValidatorSet(ctx, latestHeight)
	if err != nil {
		return nil, fmt.Errorf("get validator set: %w", err)
	}
	if vs.Height != latestHeight {
		return nil, fmt.Errorf("mismatch: latest height %d, validator set height %d", latestHeight, vs.Height)
	}

	val, ok := vs.Set[validatorAddr]
	if !ok {
		return nil, fmt.Errorf("validator (%s) not in latest validator set (%d)", validatorAddr, vs.Height)
	}

	c := &Challenge{
		ChainID:          s.chain.ID(),
		ValidatorAddress: val.Address,
		PubKeyBytes:      val.PubKeyBytes,
		PubKeyType:       val.PubKeyType,
		PaymentAddress:   paymentAddr,
		Challenge:        cryptoutil.RandomBytes(32),
	}

	if err := s.store.InsertChallenge(ctx, c); err != nil {
		return nil, fmt.Errorf("insert challenge: %w", err)
	}

	eztrc.Tracef(ctx, "issuing challenge ID %s", c.ID)

	return c, nil
}

func (s *CoreService) Register(ctx context.Context, challengeID string, signature []byte) (_ *Validator, err error) {
	ctx = trc.PrefixContextf(ctx, "[Register]")

	defer func() {
		switch {
		case err == nil:
			eztrc.Tracef(ctx, "Register: success")
			metrics.RegisterRequestsTotal.WithLabelValues(s.chain.ID(), "register", "success").Inc()
		case err != nil:
			eztrc.Tracef(ctx, "Register: error: %v", err)
			metrics.RegisterRequestsTotal.WithLabelValues(s.chain.ID(), "register", "error").Inc()
		}
	}()

	eztrc.Tracef(ctx, "challenge ID %s", challengeID)

	var validatorSet *chain.ValidatorSet
	{
		latestHeight, err := s.chain.LatestHeight(ctx)
		if err != nil {
			return nil, fmt.Errorf("get latest height: %w", err)
		}

		vs, err := s.chain.ValidatorSet(ctx, latestHeight)
		if err != nil {
			return nil, fmt.Errorf("get validator set: %w", err)
		}

		validatorSet = vs
	}

	var validator *Validator
	if err := s.store.Transact(ctx, func(tx store.Store) error {
		challenge, err := s.store.SelectChallenge(ctx, challengeID)
		if err != nil {
			return fmt.Errorf("retrieve challenge: %w", err)
		}

		defer func() {
			if err := s.store.DeleteChallenge(ctx, challenge.ID.String()); err != nil {
				eztrc.Errorf(ctx, "delete failed challenge %s from validator %s: %v", challengeID, challenge.ValidatorAddress, err)
			}
		}()

		msg := mekabuild.RegisterChallengeSignBytes(challenge.ChainID, challenge.Challenge)
		if err := s.chain.VerifySignature(ctx, challenge.PubKeyType, challenge.PubKeyBytes, msg, signature); err != nil {
			return err
		}

		validatorSetValidator, ok := validatorSet.Set[challenge.ValidatorAddress]
		if !ok {
			return fmt.Errorf("validator %q not present in validator set", challenge.ValidatorAddress)
		}

		validator = &Validator{
			ChainID:        challenge.ChainID,
			Address:        challenge.ValidatorAddress,
			Moniker:        validatorSetValidator.Moniker,
			PubKeyBytes:    challenge.PubKeyBytes,
			PubKeyType:     challenge.PubKeyType,
			PaymentAddress: challenge.PaymentAddress,
		}

		if err := s.store.UpsertValidator(ctx, validator); err != nil {
			return fmt.Errorf("update validator: %w", err)
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return validator, nil
}

func (s *CoreService) Build(
	ctx context.Context,
	height int64,
	validatorAddr string,
	maxBytes, maxGas int64,
	txs [][]byte,
	signature []byte,
) (_ [][]byte, _ string, err error) {
	tr := trc.PrefixTracef(trc.FromContext(ctx), "[Build]")
	chainID := s.chain.ID()

	defer func() {
		result := boolString(err == nil, "success", "error")
		metrics.BuildRequestsTotal.WithLabelValues(chainID, result).Inc()
		metrics.ValidatorRequestsTotal.WithLabelValues(chainID, validatorAddr, "Build", result).Inc()
		metrics.ValidatorLastBuildTimestamp.WithLabelValues(chainID, validatorAddr, result).Set(float64(time.Now().UTC().Unix()))
	}()

	tr.Tracef("height %d", height)
	tr.Tracef("validator addr %s", validatorAddr)
	tr.Tracef("max bytes %d, max gas %d, tx count %d", maxBytes, maxGas, len(txs))
	tr.Tracef("signature %dB", len(signature))

	var auction *Auction
	var proposer *Validator
	var allBids []*Bid
	{
		// Run an atomic transaction to verify and claim the auction.
		if err := s.store.Transact(ctx, func(tx store.Store) error {
			// Verify we can build this auction.
			{
				a, v, err := verifyAuction(ctx, s.chain, height, 2, tx)
				if err != nil {
					return fmt.Errorf("verify auction: %w", err)
				}

				auction = a
				proposer = v
			}

			// Verify request signature came from the correct proposer.
			{
				// SECURITY 🚨 This authenticates the request with the
				// validator's public key that we received when they registered.
				// Since the validator address is derived by us from the public
				// key and we verify a signature here, a rogue actor can't
				// impersonate a validator by giving us a validator address that
				// they don't own.
				txsHash := mekabuild.HashTxs(txs...)
				msg := mekabuild.BuildBlockRequestSignBytes(chainID, height, validatorAddr, maxBytes, maxGas, txsHash)
				if err := s.chain.VerifySignature(ctx, proposer.PubKeyType, proposer.PubKeyBytes, msg, signature); err != nil {
					return err
				}
			}

			// Mark the auction as finished and UPSERT to the store.
			{
				auction.FinishedAt = time.Now().UTC()
				if err := tx.UpsertAuction(ctx, auction); err != nil {
					return fmt.Errorf("finish auction: %w", err)
				}
			}

			// Fetch the bids for the auction.
			{
				b, err := tx.ListBids(ctx, chainID, height)
				if err != nil {
					return fmt.Errorf("get auction bids: %w", err)
				}

				allBids = b
			}

			// Good.
			return nil
		}); err != nil {
			return nil, "", fmt.Errorf("claim failed: %w", err)
		}

		// If bidders made bids sending payments to a proposer which is no
		// longer the actual proposer for the block, then we should fail the
		// auction.
		if auction.ValidatorAddress != proposer.Address {
			err := fmt.Errorf("auction failed because proposer changed: was %s, now %s", auction.ValidatorAddress, proposer.Address)
			return nil, "", err
		}
	}

	{
		tr.Tracef("total bid count %d", len(allBids))
		for _, bid := range allBids {
			tr := trc.PrefixTracef(tr, "%s: ", bid.ID)
			for _, tx := range bid.Txs {
				tr.Tracef(cryptoutil.HashTx(tx))
			}
		}

		tr.Tracef("mempool tx count %d", len(txs))
		for _, tx := range txs {
			tr := trc.PrefixTracef(tr, "mempool: ")
			tr.Tracef(cryptoutil.HashTx(tx))
		}
	}

	var txBundles []*txBundle
	{
		// Pick the winning bids for the auction. Those bids establish an implicit,
		// ordered set of transactions to be included in the block. The original
		// mempool transactions which were not included in those bids are also
		// computed and returned.
		winningBids, losingBids, remainingTxs, err := computeOrder(ctx, s.chain, auction, allBids, txs)
		if err != nil {
			return nil, "", fmt.Errorf("compute winning bids: %w", err)
		}

		tr.Tracef("winning bid count %d, remaining tx count %d", len(winningBids), len(remainingTxs))

		// Select transactions to go in the block, respecting capacity limits.
		bs, acceptedBids, rejectedBids, usedBytes, usedGas := selectTransactions(ctx, s.chain, winningBids, remainingTxs, maxBytes, maxGas)

		tr.Tracef("winning bid count %d, losing bid count %d", len(winningBids), len(losingBids))
		tr.Tracef("remaining tx count %d", len(remainingTxs))
		tr.Tracef("accepted winning bid count %d, rejected winning bid count %d", len(acceptedBids), len(rejectedBids))
		tr.Tracef("ultimate block tx count %d", len(bs))
		tr.Tracef("%d/%d bytes, %d/%d gas", usedBytes, maxBytes, usedGas, maxGas)

		// Both computeOrder and selectTransactions mutate each bid.State as they partition into winning, losing,
		// accepted and rejected groups for tracing.
		if err := s.store.UpdateBids(ctx, allBids...); err != nil {
			return nil, "", fmt.Errorf("update bids state: %w", err)
		}

		txBundles = bs
	}

	var blockTxs [][]byte
	var validatorPayment int64
	var mekatekPayment int64
	{
		var sources []string
		for _, b := range txBundles {
			blockTxs = append(blockTxs, b.txs...)
			for range b.txs {
				sources = append(sources, b.source)
			}
			validatorPayment += b.validatorPayment
			mekatekPayment += b.mekatekPayment
		}

		tr.Tracef("block tx count %d", len(blockTxs))
		for i, tx := range blockTxs {
			tr.Tracef(" - %d/%d: %s (%s)", i+1, len(blockTxs), cryptoutil.HashTx(tx), sources[i])
		}
		tr.Tracef("%d %s to validator", validatorPayment, auction.PaymentDenom)
		tr.Tracef("%d %s to mekatek", mekatekPayment, auction.PaymentDenom)
	}

	metrics.BlocksTotal.WithLabelValues(chainID).Inc()
	metrics.BlockTxsTotal.WithLabelValues(chainID).Add(float64(len(blockTxs)))
	metrics.PaymentsTotal.WithLabelValues(chainID, auction.PaymentDenom, "validator").Add(float64(validatorPayment))
	metrics.PaymentsTotal.WithLabelValues(chainID, auction.PaymentDenom, "mekatek").Add(float64(mekatekPayment))

	tr.Tracef("success")

	payment := fmt.Sprintf("%d%s", validatorPayment, auction.PaymentDenom)
	return blockTxs, payment, nil
}

func (s *CoreService) BuildV1(
	ctx context.Context,
	buildHeight int64,
	validatorAddr string,
	maxBytes, maxGas int64,
	txs [][]byte,
	signature []byte,
) (_ [][]byte, _ string, err error) {
	ctx = trc.PrefixContextf(ctx, "[BuildV1]")
	chainID := s.chain.ID()

	defer func() {
		result := boolString(err == nil, "success", "error")
		metrics.BuildRequestsTotal.WithLabelValues(chainID, result).Inc()
		metrics.ValidatorRequestsTotal.WithLabelValues(chainID, validatorAddr, "Build", result).Inc()
		metrics.ValidatorLastBuildTimestamp.WithLabelValues(chainID, validatorAddr, result).Set(float64(time.Now().UTC().Unix()))
	}()

	eztrc.Tracef(ctx, "build height %d", buildHeight)
	eztrc.Tracef(ctx, "validator addr %s", validatorAddr)
	eztrc.Tracef(ctx, "max bytes %d, max gas %d, tx count %d", maxBytes, maxGas, len(txs))
	eztrc.Tracef(ctx, "signature %dB", len(signature))

	// Verify we operate on the chain, and capture payment metadata.
	var mekatekPaymentAddress string
	var paymentDenom string
	{
		c, err := s.store.SelectChain(ctx, chainID)
		if err != nil {
			return nil, "", fmt.Errorf("query for chain: %w", err)
		}

		mekatekPaymentAddress = c.MekatekPaymentAddress
		paymentDenom = c.PaymentDenom
	}

	// Get a (valid) valset for the height, and make sure the caller can build it.
	var latestHeightValset *chain.ValidatorSet
	var buildHeightProposer *chain.Validator // latestHeight + 1
	{
		latestHeight, err := s.chain.LatestHeight(ctx)
		if err != nil {
			return nil, "", fmt.Errorf("get latest height: %w", err)
		}

		// TODO: should we enforce build height == latestHeight + 1?
		vs, err := s.chain.ValidatorSet(ctx, latestHeight)
		if err != nil {
			return nil, "", fmt.Errorf("get validator set: %w", err)
		}
		if vs.Height != latestHeight {
			return nil, "", fmt.Errorf("mismatch: latest height %d, validator set height %d", latestHeight, vs.Height)
		}

		minHeight := latestHeight
		maxHeight := latestHeight + 2

		eztrc.Tracef(ctx, "heights: latest %d, build %d, max %d", latestHeight, buildHeight, maxHeight)

		if buildHeight < minHeight {
			return nil, "", fmt.Errorf("%s/%d: %w", chainID, buildHeight, ErrAuctionTooOld)
		}

		if buildHeight > maxHeight {
			return nil, "", fmt.Errorf("%s/%d: %w", chainID, buildHeight, ErrAuctionTooNew)
		}

		p, err := s.chain.PredictProposer(ctx, vs, buildHeight)
		if err != nil {
			return nil, "", fmt.Errorf("predict proposer for build height %d: %w", buildHeight, err)
		}

		if p.Address != validatorAddr {
			return nil, "", fmt.Errorf("wrong proposer %q for height %d, want %q", validatorAddr, buildHeight, p.Address)
		}

		latestHeightValset = vs
		buildHeightProposer = p
	}

	// Verify the build request has been signed by the correct proposer.
	{
		txsHash := mekabuild.HashTxs(txs...)
		msg := mekabuild.BuildBlockRequestSignBytes(chainID, buildHeight, validatorAddr, maxBytes, maxGas, txsHash)
		if err := s.chain.VerifySignature(ctx, buildHeightProposer.PubKeyType, buildHeightProposer.PubKeyBytes, msg, signature); err != nil {
			return nil, "", err
		}
	}

	// Register the proposing validator in the store, or update the registration
	// if they're already in there.
	{
		v := &store.Validator{
			ChainID:        chainID,
			Address:        buildHeightProposer.Address,
			Moniker:        buildHeightProposer.Moniker,
			PubKeyBytes:    buildHeightProposer.PubKeyBytes,
			PubKeyType:     buildHeightProposer.PubKeyType,
			PaymentAddress: buildHeightProposer.PaymentAddress,
		}
		if err := s.store.UpsertValidator(ctx, v); err != nil {
			return nil, "", fmt.Errorf("ensure validator is registered: %w", err)
		}
	}

	// Claim the auction, and get any submitted bids.
	var auction *store.Auction
	var bids []*store.Bid
	if err := s.store.Transact(ctx, func(tx store.Store) error {
		a, err := tx.SelectAuction(ctx, chainID, buildHeight)
		switch {
		case err == nil:
			eztrc.Tracef(ctx, "auction found")

		case errors.Is(err, store.ErrNotFound):
			eztrc.Tracef(ctx, "auction not found, calculating allocation and creating")

			registered, err := tx.ListValidators(ctx, chainID)
			if err != nil {
				return fmt.Errorf("fetch registered validators: %w", err)
			}

			var registeredPower int64
			for _, v := range registered {
				if v, ok := latestHeightValset.Set[v.Address]; ok {
					eztrc.Tracef(ctx, "registered validator %s has voting power %d", v.Address, v.VotingPower)
					registeredPower += v.VotingPower
				}
			}

			const allocation = FixedAllocation

			eztrc.Tracef(ctx, "power: registered %d, total %d, allocation %.3f", registeredPower, latestHeightValset.TotalPower, allocation)

			a = &store.Auction{
				ChainID:                 chainID,
				Height:                  buildHeight,
				ValidatorAddress:        buildHeightProposer.Address,
				ValidatorAllocation:     allocation,
				ValidatorPaymentAddress: buildHeightProposer.PaymentAddress,
				MekatekPaymentAddress:   mekatekPaymentAddress,
				PaymentDenom:            paymentDenom,
				RegisteredPower:         registeredPower,
				TotalPower:              latestHeightValset.TotalPower,
			}

			if err := tx.UpsertAuction(ctx, a); err != nil {
				return fmt.Errorf("register auction: %w", err)
			}

		case err != nil:
			eztrc.Errorf(ctx, "fetch auction: %v", err)
			return fmt.Errorf("fetch auction: %w", err)
		}

		if !a.FinishedAt.IsZero() {
			eztrc.Tracef(ctx, "auction was finished at %s", traceTime(a.FinishedAt))
			return ErrAuctionFinished
		}

		if want, have := buildHeightProposer.Address, a.ValidatorAddress; want != have {
			eztrc.Errorf(ctx, "proposer for height (%s) is different than validator for auction (%s)", want, have)
			return fmt.Errorf("mismatched validators: want %s, have %s", want, have)
		}

		a.FinishedAt = time.Now().UTC()
		if err := tx.UpsertAuction(ctx, a); err != nil {
			return fmt.Errorf("finish auction: %w", err)
		}

		b, err := tx.ListBids(ctx, chainID, buildHeight)
		if err != nil {
			return fmt.Errorf("get auction bids: %w", err)
		}

		auction = a
		bids = b

		return nil
	}); err != nil {
		return nil, "", fmt.Errorf("claim failed: %w", err)
	}

	// Trace some information about the bids.
	{
		eztrc.Tracef(ctx, "total bid count %d", len(bids))
		for _, bid := range bids {
			ctx := trc.PrefixContextf(ctx, "%s: ", bid.ID)
			for _, tx := range bid.Txs {
				eztrc.Tracef(ctx, cryptoutil.HashTx(tx))
			}
		}

		eztrc.Tracef(ctx, "mempool tx count %d", len(txs))
		for _, tx := range txs {
			ctx := trc.PrefixContextf(ctx, "mempool: ")
			eztrc.Tracef(ctx, cryptoutil.HashTx(tx))
		}
	}

	// Select the tx bundles that will form the block.
	var txBundles []*txBundle
	{
		// Compute a priority order of valid bids, then add any remaining
		// mempool transactions. This will be the block.
		winningBids, losingBids, remainingTxs, err := computeOrder(ctx, s.chain, auction, bids, txs)
		if err != nil {
			return nil, "", fmt.Errorf("compute block order: %w", err)
		}

		// Make sure the block respects capacity limits (e.g. bytes and gas) and set bid states.
		bs, acceptedBids, rejectedBids, usedBytes, usedGas := selectTransactions(ctx, s.chain, winningBids, remainingTxs, maxBytes, maxGas)

		eztrc.Tracef(ctx, "winning bid count %d, losing bid count %d", len(winningBids), len(losingBids))
		eztrc.Tracef(ctx, "remaining tx count %d", len(remainingTxs))
		eztrc.Tracef(ctx, "accepted winning bid count %d, rejected winning bid count %d", len(acceptedBids), len(rejectedBids))
		eztrc.Tracef(ctx, "ultimate block tx count %d", len(bs))
		eztrc.Tracef(ctx, "%d/%d bytes, %d/%d gas", usedBytes, maxBytes, usedGas, maxGas)

		// Both computeOrder and selectTransactions mutate each bid.State as they partition into winning, losing,
		// accepted and rejected groups for tracing.
		if err := s.store.UpdateBids(ctx, bids...); err != nil {
			return nil, "", fmt.Errorf("update bids state: %w", err)
		}

		txBundles = bs
	}

	// Flatten the tx bundles and compute the payments.
	var blockTxs [][]byte
	var validatorPayment int64
	var mekatekPayment int64
	{
		var sources []string
		for _, b := range txBundles {
			blockTxs = append(blockTxs, b.txs...)
			for range b.txs {
				sources = append(sources, b.source)
			}
			validatorPayment += b.validatorPayment
			mekatekPayment += b.mekatekPayment
		}

		eztrc.Tracef(ctx, "block tx count %d", len(blockTxs))
		for i, tx := range blockTxs {
			eztrc.Tracef(ctx, " - %d/%d: %s (%s)", i+1, len(blockTxs), cryptoutil.HashTx(tx), sources[i])
		}
		eztrc.Tracef(ctx, "%d %s to validator", validatorPayment, auction.PaymentDenom)
		eztrc.Tracef(ctx, "%d %s to mekatek", mekatekPayment, auction.PaymentDenom)
	}

	// Update metrics.
	{
		metrics.BlocksTotal.WithLabelValues(chainID).Inc()
		metrics.BlockTxsTotal.WithLabelValues(chainID).Add(float64(len(blockTxs)))
		metrics.PaymentsTotal.WithLabelValues(chainID, auction.PaymentDenom, "validator").Add(float64(validatorPayment))
		metrics.PaymentsTotal.WithLabelValues(chainID, auction.PaymentDenom, "mekatek").Add(float64(mekatekPayment))
	}

	eztrc.Tracef(ctx, "success")

	payment := fmt.Sprintf("%d%s", validatorPayment, auction.PaymentDenom)
	return blockTxs, payment, nil
}

//
//
//

// evaluateBid processes a bid to determine if it is valid, which may include
// mutating fields of the bid. If the error is nil, then the bid is valid, but
// has been changed, and needs to be updated in the store. If the error is
// non-nil, the bid is no longer valid and should be thrown away.
func evaluateBid(
	ctx context.Context,
	c chain.Chain,
	auction *store.Auction,
	bid *store.Bid,
) (err error) {
	id := bid.ID.String()
	if bid.ID.IsNil() {
		id = "(new)"
	}

	ctx = trc.PrefixContextf(ctx, "[evaluate bid %s]", id)

	defer func() {
		switch {
		case err != nil:
			eztrc.Tracef(ctx, "rejected: %v", err)
			metrics.BidsEvaluatedTotal.WithLabelValues(auction.ChainID, "evaluate failed").Inc()
		case err == nil:
			eztrc.Tracef(ctx, "validated")
			metrics.BidsEvaluatedTotal.WithLabelValues(auction.ChainID, "validated").Inc()
		}
	}()

	// Each bid will pay some amount of denom to us, and some to the validator.
	// Those payments will come from a one or more addresses.
	var (
		validatorPayment = int64(0)
		mekatekPayment   = int64(0)
		payments         []Payment
	)

	// A bid contains N transactions.
	for i, txb := range bid.Txs {
		ctx := trc.PrefixContextf(ctx, "bid tx %d/%d:", i+1, len(bid.Txs))

		tx, err := c.DecodeTransaction(ctx, txb)
		if err != nil {
			eztrc.Tracef(ctx, "decode failed: %v", err)
			continue
		}

		txbNormalized, err := c.EncodeTransaction(ctx, tx)
		if err != nil {
			eztrc.Tracef(ctx, "re-encode failed: %v", err)
			continue
		}

		if !bytes.Equal(txb, txbNormalized) {
			eztrc.Tracef(ctx, "normalization changed %s -> %s", cryptoutil.HashTx(txb), cryptoutil.HashTx(txbNormalized))
			metrics.BidTxsNormalizedTotal.WithLabelValues(auction.ChainID).Inc()
		}

		bid.Txs[i] = txbNormalized

		// A transaction contains N messages.
		msgs := tx.Messages()
		for j, msg := range msgs {
			ctx := trc.PrefixContextf(ctx, "msg %d/%d:", j+1, len(msgs))

			// Each message may contain payments.
			src, dst, amount, err := c.GetPayment(ctx, msg, auction.PaymentDenom)
			if err != nil {
				eztrc.Tracef(ctx, "ignoring %T: %v", msg, err)
				continue
			}

			// We only care about specific payments.
			var (
				toValidator = sameAddr(dst, auction.ValidatorPaymentAddress)
				toMekatek   = sameAddr(dst, auction.MekatekPaymentAddress)
			)

			switch {
			case toValidator:
				eztrc.Tracef(ctx, "%s send %d to %s (validator)", src, amount, dst)
				payments = append(payments, Payment{From: src, To: dst, Amount: amount})
				validatorPayment += amount
			case toMekatek:
				eztrc.Tracef(ctx, "%s send %d to %s (mekatek)", src, amount, dst)
				payments = append(payments, Payment{From: src, To: dst, Amount: amount})
				mekatekPayment += amount
			default:
				eztrc.Tracef(ctx, "%s send %d to %s (someone): ignoring", src, amount, dst)
			}
		}
	}

	totalPayment := validatorPayment + mekatekPayment

	eztrc.Tracef(ctx, "validator (%s) +%d %s", auction.ValidatorPaymentAddress, validatorPayment, auction.PaymentDenom)
	eztrc.Tracef(ctx, "mekatek (%s) +%d %s", auction.MekatekPaymentAddress, mekatekPayment, auction.PaymentDenom)
	for _, p := range payments {
		eztrc.Tracef(ctx, "searcher (%s) -%d %s", p.From, p.Amount, auction.PaymentDenom)
	}

	// Ensure the bid has the correct overall payment allocation(s).
	var priority int64
	{
		if totalPayment <= 0 {
			return fmt.Errorf("%w: no payments", ErrInvalidRequest)
		}

		var (
			wantValidatorAllocation = auction.ValidatorAllocation
			wantMekatekAllocation   = 1 - auction.ValidatorAllocation
			haveValidatorAllocation = float64(validatorPayment) / float64(totalPayment)
			haveMekatekAllocation   = float64(mekatekPayment) / float64(totalPayment)
			allocationTolerance     = 0.01 // TODO
			validatorAllocationDiff = math.Abs(wantValidatorAllocation - haveValidatorAllocation)
			mekatekAllocationDiff   = math.Abs(wantMekatekAllocation - haveMekatekAllocation)
			isCorrectAllocation     = validatorAllocationDiff < allocationTolerance && mekatekAllocationDiff < allocationTolerance
		)
		if !isCorrectAllocation {
			return fmt.Errorf("payment allocation %.3f/%.3f doesn't satisfy %.3f/%.3f", haveValidatorAllocation, haveMekatekAllocation, wantValidatorAllocation, wantMekatekAllocation)
		}

		eztrc.Tracef(ctx, "payment allocation %.3f/%.3f satisfies %.3f/%.3f", haveValidatorAllocation, haveMekatekAllocation, wantValidatorAllocation, wantMekatekAllocation)

		priority = totalPayment
	}

	bid.Priority = priority
	bid.MekatekPayment = mekatekPayment
	bid.ValidatorPayment = validatorPayment
	bid.Payments = payments

	return nil
}

//
//
//

func verifyAuction(
	ctx context.Context,
	c chain.Chain,
	height int64,
	maxHeightOffset int64,
	tx store.Store,
) (*store.Auction, *store.Validator, error) {
	ctx = trc.PrefixContextf(ctx, "[verify auction]")

	chainID := c.ID()
	eztrc.Tracef(ctx, "chain ID %s", chainID)
	eztrc.Tracef(ctx, "height %d, max height offset %d", height, maxHeightOffset)

	// Verify we operate on chain.
	var ch *store.Chain
	{
		c, err := tx.SelectChain(ctx, chainID)
		if err != nil {
			return nil, nil, fmt.Errorf("query for chain: %w", err)
		}

		ch = c
	}

	// Get a (valid) valset for the height, and make sure the auctionProposer is registered.
	var (
		currentValidatorSet *chain.ValidatorSet
		auctionProposer     *store.Validator
	)
	{
		latestHeight, err := c.LatestHeight(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("get latest height: %w", err)
		}

		eztrc.Tracef(ctx, "latest height %d", latestHeight)

		vs, err := c.ValidatorSet(ctx, latestHeight)
		if err != nil {
			return nil, nil, fmt.Errorf("get validator set: %w", err)
		}
		if vs.Height != latestHeight {
			return nil, nil, fmt.Errorf("mismatch: latest height %d, validator set height %d", latestHeight, vs.Height)
		}

		minHeight := latestHeight
		maxHeight := latestHeight + maxHeightOffset
		eztrc.Tracef(ctx, "latest height %d, max height %d", latestHeight, maxHeight)

		if height < minHeight {
			return nil, nil, fmt.Errorf("%s/%d: %w", chainID, height, ErrAuctionTooOld)
		}

		if height > maxHeight {
			return nil, nil, fmt.Errorf("%s/%d: %w", chainID, height, ErrAuctionTooNew)
		}

		p, err := c.PredictProposer(ctx, vs, height)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to predict proposer: %w", err)
		}

		v, err := tx.SelectValidator(ctx, chainID, p.Address)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				err = ErrAuctionUnavailable
			}
			return nil, nil, fmt.Errorf("proposer %s not registered: %w", p.Address, err)
		}

		currentValidatorSet = vs
		auctionProposer = v
	}

	// If the auction is in the store, get it. Otherwise, create it.
	var auction *store.Auction
	{
		a, err := tx.SelectAuction(ctx, chainID, height)

		switch {
		case err == nil:
			eztrc.Tracef(ctx, "auction found")

		case errors.Is(err, store.ErrNotFound):
			eztrc.Tracef(ctx, "auction not found, calculating allocation and creating")

			registered, err := tx.ListValidators(ctx, chainID)
			if err != nil {
				return nil, nil, fmt.Errorf("fetch registered validators: %w", err)
			}

			var registeredPower int64
			for _, v := range registered {
				if v, ok := currentValidatorSet.Set[v.Address]; ok {
					eztrc.Tracef(ctx, "registered validator %s has voting power %d", v.Address, v.VotingPower)
					registeredPower += v.VotingPower
				}
			}

			const allocation = FixedAllocation

			eztrc.Tracef(ctx, "power: registered %d, total %d, allocation %.3f", registeredPower, currentValidatorSet.TotalPower, allocation)

			a = &store.Auction{
				ChainID:                 chainID,
				Height:                  height,
				ValidatorAddress:        auctionProposer.Address,
				ValidatorAllocation:     allocation,
				ValidatorPaymentAddress: auctionProposer.PaymentAddress,
				MekatekPaymentAddress:   ch.MekatekPaymentAddress,
				PaymentDenom:            ch.PaymentDenom,
				RegisteredPower:         registeredPower,
				TotalPower:              currentValidatorSet.TotalPower,
			}

			if err := tx.UpsertAuction(ctx, a); err != nil {
				return nil, nil, fmt.Errorf("register auction: %w", err)
			}

		case err != nil:
			eztrc.Errorf(ctx, "fetch auction: %v", err)
			return nil, nil, fmt.Errorf("fetch auction: %w", err)
		}

		if !a.FinishedAt.IsZero() {
			eztrc.Tracef(ctx, "auction was finished at %s", traceTime(a.FinishedAt))
			return nil, nil, ErrAuctionFinished
		}

		if want, have := auctionProposer.Address, a.ValidatorAddress; want != have {
			eztrc.Errorf(ctx, "expected proposer (%s) is different than recorded validator (%s)", want, have)
			return nil, nil, fmt.Errorf("mismatched validators: want %s, have %s", want, have)
		}

		auction = a
	}

	return auction, auctionProposer, nil
}

//
//
//

type txBundle struct {
	source           string
	txs              [][]byte
	validatorPayment int64
	mekatekPayment   int64
}

func selectTransactions(
	ctx context.Context,
	c chain.Chain,
	winningBids []*Bid,
	txs [][]byte,
	maxBytes, maxGas int64,
) ([]*txBundle, []*Bid, []*Bid, int64, int64) {
	ctx = trc.PrefixContextf(ctx, "[select transactions]")

	if maxBytes == -1 {
		maxBytes = math.MaxInt64
	}

	if maxGas == -1 {
		maxGas = math.MaxInt64
	}

	// The goal is to fill the block, up to the provided limits. The bids are
	// first priority, then the txs.
	//
	// It's OK to reject a bid with higher priority if it would cause the block
	// to exceed its limits, but accept a subsequent bid (or tx) with lower
	// priority if it's smaller. In fact this strategy is necessary, to prevent
	// heavy bids/txs from effectively DoSing participation in a block.
	var (
		bundles              []*txBundle
		acceptedBids         []*Bid
		rejectedBids         []*Bid
		mempoolTxAcceptCount int
		mempoolTxRejectCount int
		totalBytes           int64
		totalGas             int64
	)

	// Bids, or rather their txs, must be accepted or rejected atomically.
	for i, eb := range winningBids {
		var (
			bidBytes int64
			bidGas   int64
		)
		for j, txb := range eb.Txs {
			ctx := trc.PrefixContextf(ctx, "bid %s (%d/%d) tx %d/%d", eb.ID, i+1, len(winningBids), j+1, len(eb.Txs))

			txBytes, err := getTxBytes(ctx, c, txb)
			if err != nil {
				eztrc.Tracef(ctx, "get bytes: %v", err)
				continue
			}

			txGas, err := getTxGas(ctx, c, txb)
			if err != nil {
				eztrc.Tracef(ctx, "get gas: %v", err)
				continue
			}

			bidBytes += txBytes
			bidGas += txGas
		}

		var (
			tooManyBytes = totalBytes+bidBytes > maxBytes
			tooMuchGas   = totalGas+bidGas > maxGas
			rejectBid    = tooManyBytes || tooMuchGas
		)
		if rejectBid {
			eztrc.Tracef(ctx, "bid %s (%d/%d) rejected, too many bytes (%d) %v, too much gas (%d) %v", eb.ID, i+1, len(winningBids), bidBytes, tooManyBytes, bidGas, tooMuchGas)
			eb.State = store.BidStateRejected
			rejectedBids = append(rejectedBids, eb)
			continue
		}

		eztrc.Tracef(ctx, "bid %s (%d/%d) ACCEPTED, priority %d, bytes %d, gas %d, tx count %d, validator payment %d", eb.ID, i+1, len(winningBids), eb.Priority, bidBytes, bidGas, len(eb.Txs), eb.ValidatorPayment)
		eb.State = store.BidStateAccepted
		acceptedBids = append(acceptedBids, eb)
		totalBytes += bidBytes
		totalGas += bidGas

		bundles = append(bundles, &txBundle{
			source:           fmt.Sprintf("bid %s", eb.ID),
			txs:              eb.Txs,
			validatorPayment: eb.ValidatorPayment,
			mekatekPayment:   eb.MekatekPayment,
		})
	}

	// Mempool txs come next.
	for i, tx := range txs {
		ctx := trc.PrefixContextf(ctx, "mempool tx %s (%d/%d)", cryptoutil.HashTx(tx), i+1, len(txs))

		txBytes, err := getTxBytes(ctx, c, tx)
		if err != nil {
			eztrc.Tracef(ctx, "get bytes: %v", err)
			continue
		}

		txGas, err := getTxGas(ctx, c, tx)
		if err != nil {
			eztrc.Tracef(ctx, "get gas: %v", err)
			continue
		}

		var (
			tooManyBytes = totalBytes+txBytes > maxBytes
			tooMuchGas   = totalGas+txGas > maxGas
			rejectTx     = tooManyBytes || tooMuchGas
		)
		if rejectTx {
			eztrc.Tracef(ctx, "rejected: too many bytes (%d) %v, too much gas (%d) %v", txBytes, tooManyBytes, txGas, tooMuchGas)
			mempoolTxRejectCount++
			continue
		}

		eztrc.Tracef(ctx, "ACCEPTED: bytes %d, gas %d", txBytes, txGas)
		mempoolTxAcceptCount++

		totalBytes += txBytes
		totalGas += txGas

		bundles = append(bundles, &txBundle{
			source: "mempool",
			txs:    [][]byte{tx},
		})
	}

	eztrc.Tracef(ctx, "bids: accepted %d, rejected %d", len(acceptedBids), len(rejectedBids))
	eztrc.Tracef(ctx, "txs: accepted %d, rejected %d", mempoolTxAcceptCount, mempoolTxRejectCount)

	return bundles, acceptedBids, rejectedBids, totalBytes, totalGas
}

//
//
//

// computeOrder selects winning and losing bids, and appends remaining mempool
// txs, for a block. This process is constrained by the bytes, gas, etc. limits
// for the block as specified in the auction. It mutates the state field of the
// provided bids.
func computeOrder(
	ctx context.Context,
	c chain.Chain,
	auction *store.Auction,
	bids []*store.Bid,
	mempoolTxs [][]byte,
) ([]*Bid, []*Bid, [][]byte, error) {
	ctx = trc.PrefixContextf(ctx, "[compute order]")

	// Do basic validation of each bid if it hasn't been evaluated already,
	// attaching a priority. This can happen only for old bids before we stored
	// these fields in the DB.
	evaluatedBids := make([]*store.Bid, 0, len(bids))
	for _, bid := range bids {
		if bid.State == "" {
			if err := evaluateBid(ctx, c, auction, bid); err != nil {
				return nil, nil, nil, fmt.Errorf("evaluate un-evaluated bid: %w", err)
			}
		}
		evaluatedBids = append(evaluatedBids, bid)
	}

	// Capture the current balances of all relevant payment addresses.
	senderBalances := map[string]int64{}
	{
		// Get all the addrs we care about.
		queryBalances := map[string]struct{}{}
		for _, eb := range evaluatedBids {
			for _, p := range eb.Payments {
				queryBalances[p.From] = struct{}{}
			}
		}

		// Get the balance for each of those addrs.
		for addr := range queryBalances {
			balance, err := c.AccountBalance(ctx, auction.Height-1, addr, auction.PaymentDenom)
			if err != nil {
				eztrc.Errorf(ctx, "get account balance for sender %s: %v", addr, err)
				continue
			}
			senderBalances[addr] = balance
		}
	}

	// Sort the slice so the highest-payment bids are at the top.
	sort.SliceStable(evaluatedBids, func(i, j int) bool {
		var (
			b1, b2 = evaluatedBids[i], evaluatedBids[j]
			p1, p2 = b1.Priority, b2.Priority
		)

		if p1 != p2 {
			return p1 > p2
		}

		return bytes.Compare(b1.ID.Bytes(), b2.ID.Bytes()) < 0
	})

	// Walk the now-sorted bids, and select the winners.
	var (
		winningBids  []*Bid
		rejectedBids []*Bid
		claimedTxs   = map[string]bool{}
	)
	for i, eb := range evaluatedBids {
		ctx := trc.PrefixContextf(ctx, "bid %s (%d/%d) [%d]", eb.ID, i+1, len(evaluatedBids), eb.Priority)

		// Bids that need to be top-of-block are rejected if there is already a top-of-block bid.
		if eb.Kind == store.BidKindTop && len(winningBids) > 0 {
			eb.State = store.BidStateRejected
			rejectedBids = append(rejectedBids, eb)
			eztrc.Tracef(ctx, "rejected, block already has a top-of-block")
			metrics.BidsEvaluatedTotal.WithLabelValues(auction.ChainID, "top of block").Inc()
			continue
		}

		// Bids with transactions that have already been "claimed" by other bids are rejected.
		var hasClaimedTransactions bool
		for _, tx := range eb.Txs {
			if _, ok := claimedTxs[cryptoutil.HashTx(tx)]; ok {
				hasClaimedTransactions = true
				break
			}
		}
		if hasClaimedTransactions {
			eb.State = store.BidStateRejected
			rejectedBids = append(rejectedBids, eb)
			eztrc.Tracef(ctx, "rejected, has claimed txs")
			metrics.BidsEvaluatedTotal.WithLabelValues(auction.ChainID, "claimed txs").Inc()
			continue
		}

		// Make a copy of the balances state.
		senderBalancesCopy := make(map[string]int64, len(senderBalances))
		for k, v := range senderBalances {
			senderBalancesCopy[k] = v
		}

		// Bids with unsatisfiable payments are rejected.
		var insufficientFunds bool
		for _, p := range eb.Payments {
			balance := senderBalancesCopy[p.From]
			result := balance - p.Amount
			if result < 0 {
				eztrc.Tracef(ctx, "payment addr %s: %v - %v = %v -- insufficient funds", p.From, balance, p.Amount, result)
				insufficientFunds = true
				break
			}
			senderBalancesCopy[p.From] = result
		}

		if insufficientFunds {
			eb.State = store.BidStateRejected
			rejectedBids = append(rejectedBids, eb)
			eztrc.Tracef(ctx, "rejected, insufficient funds")
			metrics.BidsEvaluatedTotal.WithLabelValues(auction.ChainID, "insufficient funds").Inc()
			continue
		}

		// Otherwise, the bid is a winning bid.
		winningBids = append(winningBids, eb)
		senderBalances = senderBalancesCopy // update payment addr balances
		for _, tx := range eb.Txs {         // claim txs in bid
			claimedTxs[cryptoutil.HashTx(tx)] = true
		}

		eztrc.Tracef(ctx, "accepted")
		metrics.BidsEvaluatedTotal.WithLabelValues(auction.ChainID, "won").Inc()
	}

	// Append any remaining mempool transactions which aren't already in the block.
	var remainingTxs [][]byte
	for _, tx := range mempoolTxs {
		txString := cryptoutil.HashTx(tx)
		if _, ok := claimedTxs[txString]; ok {
			continue
		}

		remainingTxs = append(remainingTxs, tx)
		claimedTxs[txString] = true
	}

	// Done.
	return winningBids, rejectedBids, remainingTxs, nil
}
