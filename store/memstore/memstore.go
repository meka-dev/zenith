package memstore

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"zenith/store"

	"github.com/gofrs/uuid"
)

type Store struct {
	mu         sync.Mutex
	bids       map[auctionKey][]*store.Bid
	auctions   map[auctionKey]*store.Auction
	challenges map[string]*store.Challenge
	validators map[validatorKey]*store.Validator
	chains     map[string]*store.Chain
}

type validatorKey struct {
	chainID string
	address string
}

type auctionKey struct {
	chainID string
	height  int64
}

var _ store.Store = (*Store)(nil)

func NewStore() *Store {
	return &Store{
		bids:       map[auctionKey][]*store.Bid{},
		auctions:   map[auctionKey]*store.Auction{},
		challenges: map[string]*store.Challenge{},
		validators: map[validatorKey]*store.Validator{},
		chains:     map[string]*store.Chain{},
	}
}

func (s *Store) Transact(ctx context.Context, tx func(store.Store) error) error {
	return tx(s)
}

func (s *Store) Ping(ctx context.Context) error {
	return nil
}

func (s *Store) Cleanup(ctx context.Context) error {
	return nil
}

func (s *Store) InsertBid(ctx context.Context, b *store.Bid) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var err error
	if b.ID, err = uuid.NewV4(); err != nil {
		return fmt.Errorf("generate bid ID: %w", err)
	}

	b.CreatedAt = time.Now().UTC()

	newBid := *b
	key := auctionKey{b.ChainID, b.Height}
	s.bids[key] = append(s.bids[key], &newBid)

	return nil
}

func (s *Store) UpdateBids(ctx context.Context, bids ...*store.Bid) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, b := range bids {
		key := auctionKey{b.ChainID, b.Height}

		for _, o := range s.bids[key] {
			if b.ID == o.ID { // update
				o.State = b.State
				o.UpdatedAt = time.Now().UTC()
				break
			}
		}
	}

	return nil
}

func (s *Store) ListBids(ctx context.Context, chainID string, height int64) ([]*store.Bid, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := auctionKey{chainID, height}
	return s.bids[key], nil
}

func (s *Store) UpsertAuction(ctx context.Context, a *store.Auction) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := auctionKey{a.ChainID, a.Height}

	existing := s.auctions[key]
	if existing != nil { // update
		existing.FinishedAt = a.FinishedAt
		return nil
	}

	// create
	a.CreatedAt = time.Now().UTC()
	newAuction := *a
	s.auctions[key] = &newAuction

	return nil
}

func (s *Store) SelectAuction(ctx context.Context, chainID string, height int64) (*store.Auction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := auctionKey{chainID, height}

	if a := s.auctions[key]; a != nil {
		return a, nil
	}

	return nil, store.ErrNotFound
}

func (s *Store) InsertChallenge(ctx context.Context, c *store.Challenge) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := uuid.NewV4()
	if err != nil {
		return fmt.Errorf("uuid gen failed: %w", err)
	}

	c.ID = id
	c.CreatedAt = time.Now()

	cc := *c
	s.challenges[c.ID.String()] = &cc

	return nil
}

func (s *Store) SelectChallenge(ctx context.Context, id string) (*store.Challenge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.challenges[id]
	if !ok {
		return nil, store.ErrNotFound
	}

	cc := *c
	return &cc, nil
}

func (s *Store) DeleteChallenge(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.challenges[id]; !ok {
		return store.ErrNotFound
	}

	delete(s.challenges, id)
	return nil
}

func (s *Store) UpsertValidator(ctx context.Context, v *store.Validator) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := validatorKey{v.ChainID, v.Address}

	existing := s.validators[key]
	if existing != nil { // update
		existing.Moniker = v.Moniker
		existing.PaymentAddress = v.PaymentAddress
		existing.UpdatedAt = time.Now().UTC()
		return nil
	}

	// create
	v.CreatedAt = time.Now().UTC()
	newValidator := *v
	s.validators[key] = &newValidator

	return nil
}

func (s *Store) SelectValidator(ctx context.Context, chainID, addr string) (*store.Validator, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := validatorKey{chainID, addr}

	if v := s.validators[key]; v != nil {
		return v, nil
	}

	return nil, store.ErrNotFound
}

func (s *Store) ListValidators(ctx context.Context, chainID string) ([]*store.Validator, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var vs []*store.Validator
	for _, v := range s.validators {
		if v.ChainID == chainID {
			vs = append(vs, v)
		}
	}

	sort.SliceStable(vs, func(i, j int) bool {
		return vs[i].Address < vs[j].Address
	})

	return vs, nil
}

func (s *Store) UpsertChain(ctx context.Context, c *store.Chain) error {
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.chains[c.ID]; ok {
		// update
		*existing = *c
		existing.UpdatedAt = now
	} else {
		// create
		newChain := *c
		newChain.CreatedAt = now
		newChain.UpdatedAt = now
		s.chains[c.ID] = &newChain
	}

	return nil
}

func (s *Store) SelectChain(ctx context.Context, id string) (*store.Chain, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if c, ok := s.chains[id]; ok {
		return c, nil
	}

	return nil, store.ErrNotFound
}

func (s *Store) ListChains(ctx context.Context) ([]*store.Chain, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	chains := make([]*store.Chain, 0, len(s.chains))
	for _, c := range s.chains {
		chains = append(chains, c)
	}

	sort.SliceStable(chains, func(i, j int) bool {
		return chains[i].ID < chains[j].ID
	})

	return chains, nil
}
