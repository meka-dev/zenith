package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mekapi/trc/eztrc"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"zenith/block"
	"zenith/cryptoutil"
	"zenith/debug"
	"zenith/store"

	"github.com/go-kit/log"
	"github.com/gorilla/mux"
	"github.com/hashicorp/go-multierror"
	"github.com/meka-dev/mekatek-go/mekabuild"
	"golang.org/x/sync/errgroup"
)

const redirectURL = "https://meka.tech/zenith" // for requests to straight up `api.mekatek.xyz`

var redirectHandler = http.RedirectHandler(redirectURL, http.StatusTemporaryRedirect)

var (
	ErrUnknownChainID     = errors.New("unknown chain ID")
	ErrNoChainID          = errors.New("no chain ID")
	ErrNoValidatorAddress = errors.New("no validator address")
	ErrNoPaymentAddress   = errors.New("no payment address")
	ErrNoChallengeID      = errors.New("no challenge ID")
	ErrNoSignature        = errors.New("no signature")
)

type Handler struct {
	router  *mux.Router
	logger  log.Logger
	manager *block.ServiceManager

	// We need direct access to the store to look up challenges during the
	// registration flow, because the registration request doesn't include a
	// chain ID. We intend to fix this by adding a chain ID field to the
	// relevant type in mekabuild, but we still need to support existing users
	// who won't be sending that information. This can be removed once we no
	// longer support the "v0" registration flow.
	//
	// TODO: remove
	store store.Store
}

func NewHandler(store store.Store, manager *block.ServiceManager, logger log.Logger) *Handler {
	s := &Handler{
		router:  mux.NewRouter(),
		store:   store, // TODO: remove
		manager: manager,
		logger:  logger,
	}

	s.router.Path("/").Handler(redirectHandler)

	s.router.Methods("GET").Path("/-/ping").HandlerFunc(s.handleGetPing)
	s.router.Methods("GET").Path("/-/panic").HandlerFunc(s.handleGetPanic)

	s.router.Methods("GET").Path("/v0/auction").HandlerFunc(s.handleGetAuctionV0)
	s.router.Methods("POST").Path("/v0/bid").HandlerFunc(s.handlePostBidV0)
	s.router.Methods("POST").Path("/v0/register").HandlerFunc(s.handlePostRegisterV0)
	s.router.Methods("POST").Path("/v0/build").HandlerFunc(s.handlePostBuildV0)

	s.router.Methods("POST").Path("/v1/build").HandlerFunc(s.handlePostBuildV1) // same API, different behavior

	s.router.Use(
		mekabuild.GunzipRequestMiddleware,
		debug.TracingMiddleware,
		debug.MetricsMiddleware,
		panicRecoveryMiddleware(s.logger), // should be after observability middlewares
		// the handler executes here
	)

	return s
}

func (s *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

//
//
//

func (s *Handler) handleGetPing(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	eg, ctx := errgroup.WithContext(ctx)
	for _, sv := range s.manager.AllServices() {
		sv := sv
		eg.Go(func() error { return sv.Ping(ctx) })
	}

	err := eg.Wait()

	switch {
	case err == nil:
		respondOK(w, r, struct{}{})
	case err != nil:
		respondError(w, r, fmt.Errorf("ping: %w", err), http.StatusInternalServerError, s.logger)
	}
}

func (s *Handler) handleGetPanic(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	eztrc.Tracef(ctx, "panicking as requested")
	panic("requested panic")
}

//
//
//

type registerRequest struct {
	// Initial apply request.
	ChainID          string `json:"chain_id"`
	ValidatorAddress string `json:"validator_address"`
	PaymentAddress   string `json:"payment_address"`

	// Second register request.
	ChallengeID string `json:"challenge_id"`
	Signature   []byte `json:"signature"`
}

func (req *registerRequest) isApplyRequest() bool { return req.ChallengeID == "" }

func (req *registerRequest) isRegisterRequest() bool { return !req.isApplyRequest() }

func (req *registerRequest) validate() error {
	var merr multiError
	switch {
	case req.isApplyRequest():
		merr.addIf(req.ChainID == "", ErrNoChainID)
		merr.addIf(req.ValidatorAddress == "", ErrNoValidatorAddress)
		merr.addIf(req.PaymentAddress == "", ErrNoPaymentAddress)
	case req.isRegisterRequest():
		merr.addIf(req.ChallengeID == "", ErrNoChallengeID)
		merr.addIf(len(req.Signature) == 0, ErrNoSignature)
	}
	return merr.yield()
}

type registerResponse struct {
	// Initial apply response.
	ChallengeID string `json:"challenge_id,omitempty"`
	Challenge   []byte `json:"challenge,omitempty"`

	// Second register response.
	Result string `json:"result,omitempty"`
}

func (s *Handler) handlePostRegisterV0(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, fmt.Errorf("decode register request: %w", err), http.StatusBadRequest, s.logger)
		return
	}

	if err := req.validate(); err != nil {
		respondError(w, r, fmt.Errorf("request invalid: %w", err), http.StatusBadRequest, s.logger)
		return
	}

	// HACK: TODO: remove
	{
		// No chain ID in register requests, so we need to look it up.
		if req.isRegisterRequest() && req.ChainID == "" {
			eztrc.Tracef(ctx, "HACK: looking up register request chain ID from store")
			c, err := s.store.SelectChallenge(ctx, req.ChallengeID)
			if err != nil {
				respondError(w, r, fmt.Errorf("look up challenge: %w", err), http.StatusInternalServerError, s.logger)
				return
			}
			req.ChainID = c.ChainID
		}
	}

	eztrc.Tracef(ctx, "chain ID %q", req.ChainID)
	eztrc.Tracef(ctx, "validator addr %q", req.ValidatorAddress)

	sv, ok := s.manager.GetService(req.ChainID)
	if !ok {
		respondError(w, r, fmt.Errorf("%s: %w", req.ChainID, ErrUnknownChainID), http.StatusBadRequest, s.logger)
		return
	}

	var resp *registerResponse
	switch {
	case req.isApplyRequest():
		eztrc.Tracef(ctx, "apply: chain ID %s, validator %s, payment addr %s", req.ChainID, req.ValidatorAddress, req.PaymentAddress)
		c, err := sv.Apply(ctx, req.ValidatorAddress, req.PaymentAddress)
		if err != nil {
			respondError(w, r, fmt.Errorf("apply: %w", err), http.StatusInternalServerError, s.logger)
			return
		}
		eztrc.Tracef(ctx, "issuing challenge ID %s", c.ID)
		resp = &registerResponse{ChallengeID: c.ID.String(), Challenge: c.Challenge}

	default: // assumed to be second register request (ง •̀_•́)ง
		eztrc.Tracef(ctx, "register: challenge ID %s", req.ChallengeID)
		if _, err := sv.Register(ctx, req.ChallengeID, req.Signature); err != nil {
			respondError(w, r, fmt.Errorf("register: %w", err), http.StatusInternalServerError, s.logger)
			return
		}
		eztrc.Tracef(ctx, "registation success")
		resp = &registerResponse{Result: "success"}
	}

	respondOK(w, r, resp)
}

//
//
//

type bidRequest struct {
	ChainID string   `json:"chain_id"`
	Height  int64    `json:"height"`
	Kind    string   `json:"kind"`
	Txs     [][]byte `json:"txs"`
}

func (req *bidRequest) validate() error {
	var merr multiError
	merr.addIf(req.ChainID == "", ErrNoChainID)
	merr.addIf(req.Height <= 0, fmt.Errorf("invalid height"))
	return merr.yield()
}

type bidResponse struct {
	ChainID  string   `json:"chain_id"`
	Height   int64    `json:"height"`
	Kind     string   `json:"kind"`
	TxHashes []string `json:"tx_hashes"`
}

func (s *Handler) handlePostBidV0(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req bidRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, fmt.Errorf("decode bid request: %w", err), http.StatusBadRequest, s.logger)
		return
	}

	if err := req.validate(); err != nil {
		respondError(w, r, fmt.Errorf("request invalid: %w", err), http.StatusBadRequest, s.logger)
		return
	}

	eztrc.Tracef(ctx, "chain ID %q", req.ChainID)
	eztrc.Tracef(ctx, "height %d", req.Height)

	sv, ok := s.manager.GetService(req.ChainID)
	if !ok {
		respondError(w, r, fmt.Errorf("%s: %w", req.ChainID, ErrUnknownChainID), http.StatusBadRequest, s.logger)
		return
	}

	bid, err := sv.Bid(ctx, req.Height, req.Kind, req.Txs)
	if err != nil {
		respondError(w, r, fmt.Errorf("bid on %s/%d: %w", req.ChainID, req.Height, err), http.StatusInternalServerError, s.logger)
		return
	}

	eztrc.Tracef(ctx, "bid on %s/%d, kind %s, tx count %d", req.ChainID, req.Height, req.Kind, len(req.Txs))

	respondOK(w, r, bidResponse{
		ChainID:  bid.ChainID,
		Height:   bid.Height,
		Kind:     string(bid.Kind),
		TxHashes: cryptoutil.HashTxs(bid.Txs),
	})
}

//
//
//

type auctionRequest struct {
	ChainID string `json:"chain_id"`
	Height  int64  `json:"height"`
}

func parseAuctionRequest(ctx context.Context, r *http.Request) (auctionRequest, error) {
	var req auctionRequest

	readBodyJSON := func() error {
		return json.NewDecoder(r.Body).Decode(&req)
	}

	parseValues := func(values url.Values) error {
		if chainID := values.Get("chain_id"); chainID != "" {
			req.ChainID = chainID // non-fatal, let `{"chain_id":"X","height":123}` + `?height=456` => chain_id=X height=456
		}
		if height, err := strconv.ParseInt(values.Get("height"), 10, 64); err == nil {
			req.Height = height // ibid.
		}
		return nil
	}

	readURLQuery := func() error {
		values, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			return fmt.Errorf("parse query data: %w", err)
		}
		return parseValues(values)
	}

	readFormData := func() error {
		body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024))
		if err != nil {
			return fmt.Errorf("read request body: %w", err)
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return fmt.Errorf("parse form data: %w", err)
		}
		return parseValues(values)
	}

	var (
		requestTypes = r.Header.Values("content-type")
		acceptTypes  = []string{"application/json", "application/x-www-form-urlencoded", "multipart/form-data"}
		bestType     = getBestMediaType(ctx, requestTypes, acceptTypes...)
	)

	switch {
	case bestType == "application/json":
		if err := readBodyJSON(); err != nil {
			return req, fmt.Errorf("decode JSON auction request: %w", err)
		}
		eztrc.Tracef(ctx, "parsed auction request from JSON request body: %v", req)

	case bestType == "application/x-www-form-urlencoded":
		if err := readFormData(); err != nil {
			return req, fmt.Errorf("decode form auction request: %w", err)
		}
		eztrc.Tracef(ctx, "parsed auction request from form data: %v", req)

	default:
		eztrc.Tracef(ctx, "request has no content-type, trying a few things...")
		if err := readBodyJSON(); err != nil {
			eztrc.Tracef(ctx, "JSON parse failed: %v", err)
		}
		if err := readFormData(); err != nil {
			eztrc.Tracef(ctx, "form parse failed: %v", err)
		}
		if err := readURLQuery(); err != nil {
			eztrc.Tracef(ctx, "query parse failed: %v", err)
		}
	}

	if err := req.validate(); err != nil {
		return req, fmt.Errorf("request invalid: %w", err)
	}

	return req, nil
}

func (req *auctionRequest) validate() error {
	var merr multiError
	merr.addIf(req.ChainID == "", ErrNoChainID)
	merr.addIf(req.Height <= 0, fmt.Errorf("invalid height"))
	return merr.yield()
}

type auctionResponse struct {
	ChainID  string    `json:"chain_id"`
	Height   int64     `json:"height"`
	Payments []payment `json:"payments"`
}

type payment struct {
	Address    string  `json:"address"`
	Allocation float64 `json:"allocation"`
	Denom      string  `json:"denom"`
}

func paymentsFor(a *block.Auction) []payment {
	return []payment{
		{
			Address:    a.ValidatorPaymentAddress,
			Allocation: a.ValidatorAllocation,
			Denom:      a.PaymentDenom,
		},
		{
			Address:    a.MekatekPaymentAddress,
			Allocation: 1 - a.ValidatorAllocation,
			Denom:      a.PaymentDenom,
		},
	}
}

func (s *Handler) handleGetAuctionV0(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	req, err := parseAuctionRequest(ctx, r)
	if err != nil {
		respondError(w, r, fmt.Errorf("parse auction request: %w", err), http.StatusBadRequest, s.logger)
		return
	}

	eztrc.Tracef(ctx, "chain ID %q", req.ChainID)
	eztrc.Tracef(ctx, "height %d", req.Height)

	sv, ok := s.manager.GetService(req.ChainID)
	if !ok {
		respondError(w, r, fmt.Errorf("%s: %w", req.ChainID, ErrUnknownChainID), http.StatusBadRequest, s.logger)
		return
	}

	auction, err := sv.Auction(ctx, req.Height)
	if err != nil {
		respondError(w, r, fmt.Errorf("get auction for %s/%d: %w", req.ChainID, req.Height, err), http.StatusInternalServerError, s.logger)
		return
	}

	payments := paymentsFor(auction)

	eztrc.Tracef(ctx, "auction for %s/%d, payments count %d", req.ChainID, req.Height, len(payments))

	respondOK(w, r, auctionResponse{
		ChainID:  auction.ChainID,
		Height:   auction.Height,
		Payments: payments,
	})
}

func getBestMediaType(ctx context.Context, inputValues []string, prioritizedValues ...string) string {
	if len(inputValues) <= 0 {
		return ""
	}

	index := map[string]struct{}{}
	slice := []string{}
	for _, v := range inputValues {
		mediaType, _, err := mime.ParseMediaType(v)
		if err != nil {
			eztrc.Tracef(ctx, "warning: request content-type %q: %v", v, err)
			continue
		}
		index[mediaType] = struct{}{}
		slice = append(slice, mediaType)
	}

	for _, v := range prioritizedValues {
		mediaType, _, err := mime.ParseMediaType(v)
		if err != nil {
			eztrc.Errorf(ctx, "programmer error: invalid content type %q", v)
			continue
		}
		if _, ok := index[mediaType]; ok {
			return mediaType
		}
	}

	return slice[0]
}

//
//
//

func validateBuildBlockRequest(req *mekabuild.BuildBlockRequest) error {
	var merr multiError
	merr.addIf(req.ChainID == "", fmt.Errorf("chain ID missing"))
	merr.addIf(req.Height <= 0, fmt.Errorf("height missing"))
	merr.addIf(len(req.Signature) <= 0, fmt.Errorf("signature missing"))
	merr.addIf(req.ValidatorAddress == "", fmt.Errorf("validator address missing"))
	return merr.yield()
}

func (s *Handler) handlePostBuildV0(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req mekabuild.BuildBlockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, fmt.Errorf("decode build block request: %w", err), http.StatusBadRequest, s.logger)
		return
	}

	if err := validateBuildBlockRequest(&req); err != nil {
		respondError(w, r, fmt.Errorf("invalid request: %w", err), http.StatusBadRequest, s.logger)
		return
	}

	eztrc.Tracef(ctx, "chain ID %s", req.ChainID)
	eztrc.Tracef(ctx, "height %d", req.Height)
	eztrc.Tracef(ctx, "validator address %s", req.ValidatorAddress)

	sv, ok := s.manager.GetService(req.ChainID)
	if !ok {
		respondError(w, r, fmt.Errorf("%s: %w", req.ChainID, ErrUnknownChainID), http.StatusBadRequest, s.logger)
		return
	}

	txs, payment, err := sv.Build(ctx, req.Height, req.ValidatorAddress, req.MaxBytes, req.MaxGas, req.Txs, req.Signature)
	if err != nil {
		respondError(w, r, fmt.Errorf("build block for %s/%d: %w", req.ChainID, req.Height, err), http.StatusInternalServerError, s.logger)
		return
	}

	eztrc.Tracef(ctx, "build OK, tx count %d, validator payment %s", len(txs), payment)

	respondOK(w, r, mekabuild.BuildBlockResponse{Txs: txs, ValidatorPayment: payment})
}

func (s *Handler) handlePostBuildV1(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req mekabuild.BuildBlockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, fmt.Errorf("decode build block request: %w", err), http.StatusBadRequest, s.logger)
		return
	}

	if err := validateBuildBlockRequest(&req); err != nil {
		respondError(w, r, fmt.Errorf("invalid request: %w", err), http.StatusBadRequest, s.logger)
		return
	}

	eztrc.Tracef(ctx, "chain ID %s", req.ChainID)
	eztrc.Tracef(ctx, "height %d", req.Height)
	eztrc.Tracef(ctx, "validator address %s", req.ValidatorAddress)

	sv, ok := s.manager.GetService(req.ChainID)
	if !ok {
		respondError(w, r, fmt.Errorf("%s: %w", req.ChainID, ErrUnknownChainID), http.StatusBadRequest, s.logger)
		return
	}

	txs, payment, err := sv.BuildV1(ctx, req.Height, req.ValidatorAddress, req.MaxBytes, req.MaxGas, req.Txs, req.Signature)
	if err != nil {
		respondError(w, r, fmt.Errorf("build block for %s/%d: %w", req.ChainID, req.Height, err), http.StatusInternalServerError, s.logger)
		return
	}

	eztrc.Tracef(ctx, "build OK, tx count %d, validator payment %s", len(txs), payment)

	respondOK(w, r, mekabuild.BuildBlockResponse{Txs: txs, ValidatorPayment: payment})
}

type multiError struct {
	merr *multierror.Error
}

func (m *multiError) addIf(b bool, err error) {
	if !b {
		return
	}

	if m.merr == nil {
		m.merr = &multierror.Error{ErrorFormat: joinErrorStrings}
	}

	m.merr = multierror.Append(m.merr, err)
}

func (m *multiError) yield() error {
	if m.merr == nil {
		return nil
	}

	return m.merr.ErrorOrNil()
}

func joinErrorStrings(errs []error) string {
	strs := make([]string, len(errs))
	for i := range errs {
		strs[i] = errs[i].Error()
	}
	return strings.Join(strs, "; ")
}
