package zcosmos

import (
	"context"
	"fmt"
	"mekapi/trc/eztrc"
	"net/http"
	"strings"
	"time"
	"zenith/chain"
	"zenith/cryptoutil"
	"zenith/metrics"

	sdk_client "github.com/cosmos/cosmos-sdk/client"
	sdk_codec "github.com/cosmos/cosmos-sdk/codec"
	sdk_crypto_types "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk_types "github.com/cosmos/cosmos-sdk/types"
	sdk_types_bech32 "github.com/cosmos/cosmos-sdk/types/bech32"
	sdk_types_query "github.com/cosmos/cosmos-sdk/types/query"
	sdk_x_bank_types "github.com/cosmos/cosmos-sdk/x/bank/types"
	sdk_x_staking_types "github.com/cosmos/cosmos-sdk/x/staking/types"
	tm_crypto "github.com/tendermint/tendermint/crypto"
	tm_crypto_ed25519 "github.com/tendermint/tendermint/crypto/ed25519"
	tm_crypto_secp256k1 "github.com/tendermint/tendermint/crypto/secp256k1"
	tm_rpc_client "github.com/tendermint/tendermint/rpc/client"
	tm_rpc_client_http "github.com/tendermint/tendermint/rpc/client/http"
	tm_types "github.com/tendermint/tendermint/types"
	"golang.org/x/sync/errgroup"
)

// NetworkConfig are the per-network parameters that are independent of state in
// the store. That means it doesn't include e.g. chain ID, full node addrs, the
// HTTP client (due to the timeout param), etc.
//
// TODO: validation?
type NetworkConfig struct {
	Network             string              // e.g. "osmosis" (optional, default build.Network)
	Bech32PrefixAccAddr string              // e.g. "osmo"
	StallThreshold      time.Duration       // typically ~minutes (default 5m)
	Codec               sdk_codec.Codec     // from the network
	TxConfig            sdk_client.TxConfig // from the network
}

type Chain struct {
	network             string
	bech32PrefixAccAddr string
	stallThreshold      time.Duration
	codec               sdk_codec.Codec
	txConfig            sdk_client.TxConfig

	chainID string
	clients *rpcClients
}

var _ chain.Chain = (*Chain)(nil)

func NewChain(
	netConf NetworkConfig,
	chainID string,
	rpcAddrs []string,
	httpClient *http.Client,
) (*Chain, error) {
	if chainID == "" {
		return nil, fmt.Errorf("chain ID required")
	}

	if len(rpcAddrs) == 0 {
		return nil, fmt.Errorf("node URIs required")
	}

	var clients []*tm_rpc_client_http.HTTP
	for _, addr := range rpcAddrs {
		c, err := tm_rpc_client_http.NewWithClient(addr, "/websocket", httpClient)
		if err != nil {
			return nil, fmt.Errorf("create client for %q: %w", addr, err)
		}
		clients = append(clients, c)
	}

	return &Chain{
		network:             netConf.Network,
		bech32PrefixAccAddr: netConf.Bech32PrefixAccAddr,
		stallThreshold:      netConf.StallThreshold,
		codec:               netConf.Codec,
		txConfig:            netConf.TxConfig,

		chainID: chainID,
		clients: &rpcClients{clients},
	}, nil
}

func (c *Chain) ID() string {
	return c.chainID
}

func (c *Chain) ValidatePaymentAddress(ctx context.Context, addr string) error {
	hrp, bz, err := sdk_types_bech32.DecodeAndConvert(addr)
	if err != nil {
		return fmt.Errorf("decode as Bech32: %w", err)
	}

	if !strings.HasPrefix(hrp, c.bech32PrefixAccAddr) {
		return fmt.Errorf("address (%s) missing prefix (%s)", addr, c.bech32PrefixAccAddr)
	}

	switch n := len(bz); n {
	case 20, 32:
		// good
	default:
		return fmt.Errorf("address length (%d) invalid: must be 20 or 32", n)
	}

	return nil
}

func (c *Chain) VerifySignature(ctx context.Context, pubKeyType string, pubKeyBytes, msg, sig []byte) error {
	pubKey, err := newPubKey(pubKeyType, pubKeyBytes)
	if err != nil {
		return err
	}

	if !pubKey.VerifySignature(msg, sig) {
		return chain.ErrBadSignature
	}

	return nil
}

func (c *Chain) DecodeTransaction(ctx context.Context, txb []byte) (chain.Transaction, error) {
	defaultTx, defaultErr := c.txConfig.TxDecoder()(txb)
	if defaultErr == nil {
		eztrc.Tracef(ctx, "decoded %s with default decoder", cryptoutil.HashTx(txb))
		return NewCosmosTransaction(txb, defaultTx)
	}

	jsonTx, jsonErr := c.txConfig.TxJSONDecoder()(txb)
	if jsonErr == nil {
		eztrc.Tracef(ctx, "decoded %s with JSON decoder", cryptoutil.HashTx(txb))
		return NewCosmosTransaction(txb, jsonTx)
	}

	return nil, fmt.Errorf("decode failed (%v, %v)", defaultErr, jsonErr)
}

func (c *Chain) EncodeTransaction(ctx context.Context, tx chain.Transaction) ([]byte, error) {
	cosmosTransaction, ok := tx.(*CosmosTransaction)
	if !ok {
		return nil, fmt.Errorf("unexpected transaction type %T", tx)
	}

	txb, err := c.txConfig.TxEncoder()(cosmosTransaction.sdktx)
	if err != nil {
		return nil, fmt.Errorf("encode failed: %w", err)
	}

	eztrc.Tracef(ctx, "encoded %s", cryptoutil.HashTx(txb))

	return txb, nil
}

func (c *Chain) AccountBalance(ctx context.Context, height int64, addr, denom string) (int64, error) {
	var accountBalance int64

	if err := c.clients.do(ctx, func(client *tm_rpc_client_http.HTTP) error {
		req := sdk_x_bank_types.QueryBalanceRequest{
			Address: addr,
			Denom:   denom,
		}

		reqBytes, err := req.Marshal()
		if err != nil {
			return fmt.Errorf("marshal query balance request: %w", err)
		}

		var (
			path = "/cosmos.bank.v1beta1.Query/Balance" // what e.g. `osmosisd query bank ...` uses
			data = reqBytes
			opts = tm_rpc_client.ABCIQueryOptions{Height: height}
		)
		abciResult, err := client.ABCIQueryWithOptions(ctx, path, data, opts)
		if err != nil {
			return fmt.Errorf("ABCI query: %w", err)
		}

		if !abciResult.Response.IsOK() {
			return fmt.Errorf("ABCI result response not OK: codespace %q, code %d, log %q", abciResult.Response.Codespace, abciResult.Response.Code, abciResult.Response.GetLog())
		}

		var response sdk_x_bank_types.QueryBalanceResponse
		if err := response.Unmarshal(abciResult.Response.Value); err != nil {
			return fmt.Errorf("unmarshal query balance response: %w", err)
		}

		switch {
		case response.GetBalance() == nil:
			eztrc.Tracef(ctx, "%s has missing balance", addr)
		case response.GetBalance().IsNil():
			eztrc.Tracef(ctx, "%s has nil balance of %s", addr, denom)
		case response.GetBalance().GetDenom() != denom:
			eztrc.Tracef(ctx, "%s gave back wrong denom %s", addr, response.Balance.GetDenom())
		case response.GetBalance().Amount.IsNil() || response.Balance.Amount.IsZero():
			eztrc.Tracef(ctx, "%s has 0 balance of %s", addr, denom)
		default:
			accountBalance = response.Balance.Amount.Int64()
		}

		return nil
	}); err != nil {
		return 0, err
	}

	return accountBalance, nil
}

func (c *Chain) LatestHeight(ctx context.Context) (int64, error) {
	var latestHeight int64

	if err := c.clients.do(ctx, func(client *tm_rpc_client_http.HTTP) error {
		status, err := client.Status(ctx)
		if err != nil {
			return fmt.Errorf("check node status: %w", err)
		}

		if status.SyncInfo.CatchingUp {
			return fmt.Errorf("node is catching up")
		}

		if age := time.Since(status.SyncInfo.LatestBlockTime); age > c.stallThreshold {
			return fmt.Errorf("node appears stalled: last block was %s ago", age.Truncate(time.Second))
		}

		latestHeight = status.SyncInfo.LatestBlockHeight
		return nil
	}); err != nil {
		return 0, err
	}

	return latestHeight, nil
}

func (c *Chain) ValidatorSet(ctx context.Context, targetHeight int64) (*chain.ValidatorSet, error) {
	defer func(begin time.Time) {
		metrics.OpWait("latest_valset", time.Since(begin))
	}(time.Now())

	if targetHeight <= 0 {
		h, err := c.LatestHeight(ctx)
		if err != nil {
			return nil, fmt.Errorf("get latest height: %w", err)
		}
		targetHeight = h
	}

	var validatorSet *chain.ValidatorSet
	if err := c.clients.do(ctx, func(client *tm_rpc_client_http.HTTP) error {
		vs, err := getValidatorSet(ctx, client, c.codec, targetHeight)
		if err != nil {
			return fmt.Errorf("get validator set at %d: %w", targetHeight, err)
		}
		validatorSet = vs
		return nil
	}); err != nil {
		return nil, err
	}

	return validatorSet, nil
}

func (c *Chain) PredictProposer(ctx context.Context, valset *chain.ValidatorSet, height int64) (*chain.Validator, error) {
	d := height - valset.Height
	if d <= 0 {
		return nil, fmt.Errorf("can only predict future proposers")
	}

	vs := &tm_types.ValidatorSet{
		Validators: make([]*tm_types.Validator, 0, len(valset.Validators)),
	}

	for _, v := range valset.Validators {
		pubKey, err := newPubKey(v.PubKeyType, v.PubKeyBytes)
		if err != nil {
			return nil, fmt.Errorf("invalid pub key: %w", err)
		}

		addr := pubKey.Address()
		if addrString := addr.String(); addrString != v.Address {
			return nil, fmt.Errorf("address mismatch, %q != %q", addrString, v.Address)
		}

		vs.Validators = append(vs.Validators, &tm_types.Validator{
			Address:          addr,
			PubKey:           pubKey,
			VotingPower:      v.VotingPower,
			ProposerPriority: v.ProposerPriority,
		})
	}

	vs.IncrementProposerPriority(int32(d))

	proposer := vs.GetProposer()
	if proposer == nil {
		return nil, fmt.Errorf("no proposer")
	}

	proposerAddr := proposer.Address.String()
	p, ok := valset.Set[proposerAddr]
	if !ok {
		return nil, fmt.Errorf("proposer %q missing", proposerAddr)
	}

	return p, nil
}

func (c *Chain) GetPayment(ctx context.Context, msg chain.Message, denom string) (src, dst string, amount int64, err error) {
	send, ok := msg.(*sdk_x_bank_types.MsgSend)
	if !ok {
		return "", "", 0, fmt.Errorf("irrelvant msg type %T: %w", msg, chain.ErrNoPayment)
	}

	if send.Amount == nil {
		return "", "", 0, fmt.Errorf("nil amount: %w", chain.ErrNoPayment)
	}

	i := send.Amount.AmountOfNoDenomValidation(denom)
	if i.IsNil() {
		return "", "", 0, fmt.Errorf("nil amount of denom: %w", chain.ErrNoPayment)
	}
	if !i.IsInt64() {
		return "", "", 0, fmt.Errorf("invalid amount of denom: %w", chain.ErrNoPayment)
	}
	if i.IsZero() {
		return "", "", 0, fmt.Errorf("zero amount of denom: %w", chain.ErrNoPayment)
	}
	if i.IsNegative() {
		return "", "", 0, fmt.Errorf("negative amount of denom: %w", chain.ErrNoPayment)
	}

	a := i.Int64()
	if a <= 0 {
		return "", "", 0, fmt.Errorf("bad amount (%d): %w", a, chain.ErrNoPayment)
	}

	return send.FromAddress, send.ToAddress, a, nil
}

//
//
//

func newPubKey(pubKeyType string, pubKeyBytes []byte) (tm_crypto.PubKey, error) {
	switch {
	case pubKeyType == tm_crypto_ed25519.KeyType && len(pubKeyBytes) == tm_crypto_ed25519.PubKeySize:
		return tm_crypto_ed25519.PubKey(pubKeyBytes), nil
	case pubKeyType == tm_crypto_secp256k1.KeyType && len(pubKeyBytes) == tm_crypto_secp256k1.PubKeySize:
		return tm_crypto_secp256k1.PubKey(pubKeyBytes), nil
	default:
		return nil, chain.ErrInvalidKey
	}
}

type testContextKey struct{}

var sequentialKey testContextKey

func getValidatorSet(ctx context.Context, client tm_rpc_client.Client, codec sdk_codec.Codec, targetHeight int64) (*chain.ValidatorSet, error) {
	defer func(began time.Time) {
		took := time.Since(began)
		eztrc.LazyTracef(ctx, "getValidatorSet took %s", took)
		metrics.OpWait("get_valset", took)
	}(time.Now())

	var (
		actualHeight  = int64(-1)
		tmValset      *tm_types.ValidatorSet
		stakingValset map[string]*sdk_x_staking_types.Validator
		sequential, _ = ctx.Value(sequentialKey).(bool)
	)

	switch {
	case sequential:
		vs, height, err := getTendermintValidatorSet(ctx, client, targetHeight)
		if err != nil {
			return nil, fmt.Errorf("get Tendermint validator set: %w", err)
		}
		tmValset = vs
		actualHeight = height

		ss, err := getStakingValidatorSet(ctx, client, codec)
		if err != nil {
			return nil, fmt.Errorf("get staking validator set: %w", err)
		}
		stakingValset = ss

	default:
		g, ctx := errgroup.WithContext(ctx)

		g.Go(func() error {
			vs, height, err := getTendermintValidatorSet(ctx, client, targetHeight)
			if err != nil {
				return fmt.Errorf("get Tendermint validator set: %w", err)
			}
			tmValset = vs
			actualHeight = height
			return nil
		})

		g.Go(func() error {
			vs, err := getStakingValidatorSet(ctx, client, codec)
			if err != nil {
				return fmt.Errorf("get staking validator set: %w", err)
			}
			stakingValset = vs
			return nil
		})

		if err := g.Wait(); err != nil {
			return nil, err
		}
	}

	var (
		totalPower = int64(0)
		validators = make([]*chain.Validator, 0, len(tmValset.Validators))
		set        = make(map[string]*chain.Validator, len(tmValset.Validators))
	)

	for _, v := range tmValset.Validators {
		totalPower += v.VotingPower

		consensusAddrHex := v.Address.String()
		sv, ok := stakingValset[consensusAddrHex]
		if !ok {
			return nil, fmt.Errorf("validator %q not in staking validator set", consensusAddrHex)
		}

		// OperatorAddress is the `osmovaloper` prefixed staking account address.
		// So we can parse that into a "base" AccAddr...
		operatorAddress, err := sdk_types.ValAddressFromBech32(sv.OperatorAddress)
		if err != nil {
			return nil, fmt.Errorf("invalid operator address: %w", err)
		}

		// ...and render it with an `osmo` prefix to make a payment address.
		paymentAddressBech32 := sdk_types.AccAddress(operatorAddress).String()

		chainValidator := &chain.Validator{
			Address:          consensusAddrHex,
			Moniker:          sv.Description.Moniker,
			PaymentAddress:   paymentAddressBech32,
			PubKeyType:       v.PubKey.Type(),
			PubKeyBytes:      v.PubKey.Bytes(),
			VotingPower:      v.VotingPower,
			ProposerPriority: v.ProposerPriority,
		}

		validators = append(validators, chainValidator)
		set[consensusAddrHex] = chainValidator
	}

	return &chain.ValidatorSet{
		Height:     actualHeight,
		Validators: validators,
		Set:        set,
		TotalPower: totalPower,
	}, nil
}

// getStakingValidatorSet is used for the validator's moniker, and, more
// importantly, their staking address, which we use as their (default, only)
// payment address.
//
// We don't pass a target height even though the ABCIQuery accepts one, because
// the staking module appears to race vs. the actual latest height of the chain,
// and frequently fails to return data for anything beyond the latest height as
// returned by the status endpoint, e.g.
//
//	$ fullnoded status | jq ...lastest_block_height  # returns H
//	$ fullnoded query staking validators --height=H+1
//	Error: rpc error:
//	  code = InvalidArgument
//	  desc = failed to load state at height H+1; (latest height: H): invalid request
//
// So.
func getStakingValidatorSet(ctx context.Context, client tm_rpc_client.Client, codec sdk_codec.Codec) (map[string]*sdk_x_staking_types.Validator, error) {
	defer func(began time.Time) {
		took := time.Since(began)
		metrics.OpWait("rpc_tm_stakingvalset", took)
		eztrc.LazyTracef(ctx, "getStakingValidatorSet took %s", took)
	}(time.Now())

	var (
		req    = sdk_x_staking_types.QueryValidatorsRequest{Pagination: &sdk_types_query.PageRequest{Limit: 1000}}
		valset = map[string]*sdk_x_staking_types.Validator{}
	)

	for {
		reqBytes, err := req.Marshal()
		if err != nil {
			return nil, fmt.Errorf("marshal query staking validators request: %w", err)
		}

		var (
			path = "/cosmos.staking.v1beta1.Query/Validators" // what `osmosisd query staking validators` uses
			data = reqBytes
			opts = tm_rpc_client.ABCIQueryOptions{} // no height
		)
		abciResult, err := client.ABCIQueryWithOptions(ctx, path, data, opts)
		if err != nil {
			return nil, fmt.Errorf("ABCI query: %w", err)
		}

		if !abciResult.Response.IsOK() {
			return nil, fmt.Errorf("ABCI result response not OK: codespace %q, code %d, log %q", abciResult.Response.Codespace, abciResult.Response.Code, abciResult.Response.GetLog())
		}

		var response sdk_x_staking_types.QueryValidatorsResponse
		if err := response.Unmarshal(abciResult.Response.Value); err != nil {
			return nil, fmt.Errorf("unmarshal query staking validators response: %w", err)
		}

		vals := response.GetValidators()
		for i := range vals {
			// Handily, the ConsensusPubKey in the staking validator represents
			// the validator's consensus address, not their staking address.
			//
			// Unfortunately, it seems that QueryValidatorsResponse.Unmarshal
			// doesn't actually parse this field, so we have to do it manually.
			var pubKey sdk_crypto_types.PubKey
			if err := codec.UnpackAny(vals[i].ConsensusPubkey, &pubKey); err != nil {
				return nil, fmt.Errorf("unpack pub key: %w", err)
			}

			// We can get an address from a pub key, which means we can link the
			// staking information to the validator from the Tendermint valset.
			consensusAddr := pubKey.Address().String()
			valset[consensusAddr] = &vals[i]
		}

		nextKey := response.GetPagination().GetNextKey()
		if nextKey == nil {
			return valset, nil
		}

		req.Pagination.Key = nextKey
	}
}

func getTendermintValidatorSet(ctx context.Context, client tm_rpc_client.Client, targetHeight int64) (*tm_types.ValidatorSet, int64, error) {
	defer func(began time.Time) {
		took := time.Since(began)
		metrics.OpWait("rpc_tm_valset", took)
		eztrc.LazyTracef(ctx, "getTendermintValidatorSet took %s", took)
	}(time.Now())

	// This method only supports page-based pagination up to size 100 :(
	var (
		actualHeight = int64(-1)
		vals         = []*tm_types.Validator{}
		heightPtr    = &targetHeight
		perPage      = 100
		page         = 1
	)

	for {
		res, err := client.Validators(ctx, heightPtr, &page, &perPage)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to get latest validator set: %w", err)
		}

		switch {
		case len(vals) == 0: // first page
			actualHeight = res.BlockHeight
		case res.BlockHeight != actualHeight:
			return nil, 0, fmt.Errorf("height discrepancy: %d, %d", actualHeight, res.BlockHeight)
		}

		vals = append(vals, res.Validators...)

		if res.Count != perPage {
			break
		}

		page++
	}

	return &tm_types.ValidatorSet{Validators: vals}, actualHeight, nil
}

//
//
//

//
//
//

type CosmosTransaction struct {
	sdktx sdk_types.Tx
	txb   []byte
}

func NewCosmosTransaction(txb []byte, tx sdk_types.Tx) (*CosmosTransaction, error) {
	if err := tx.ValidateBasic(); err != nil {
		return nil, fmt.Errorf("validate transaction: %w", err)
	}
	return &CosmosTransaction{tx, txb}, nil
}

func (t *CosmosTransaction) Messages() []chain.Message {
	var chainmsgs []chain.Message
	for _, sdkmsg := range t.sdktx.GetMsgs() {
		chainmsgs = append(chainmsgs, sdkmsg)
	}
	return chainmsgs
}

func (t *CosmosTransaction) ByteCount() (int64, error) {
	return tm_types.ComputeProtoSizeForTxs([]tm_types.Tx{t.txb}), nil
}

func (t *CosmosTransaction) GasAmount() (int64, error) {
	g, ok := t.sdktx.(interface{ GetGas() uint64 })
	if !ok {
		return 0, fmt.Errorf("not a gas transaction")
	}

	return int64(g.GetGas()), nil
}
