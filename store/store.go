package store

import (
	"context"
)

type Store interface {
	Transact(context.Context, func(Store) error) error

	Ping(ctx context.Context) error
	Cleanup(ctx context.Context) error

	InsertBid(ctx context.Context, bid *Bid) error
	UpdateBids(ctx context.Context, bids ...*Bid) error
	ListBids(ctx context.Context, chainID string, height int64) ([]*Bid, error)

	UpsertAuction(ctx context.Context, a *Auction) error
	SelectAuction(ctx context.Context, chainID string, height int64) (*Auction, error)

	InsertChallenge(ctx context.Context, c *Challenge) error
	SelectChallenge(ctx context.Context, id string) (*Challenge, error)
	DeleteChallenge(ctx context.Context, id string) error

	UpsertValidator(ctx context.Context, v *Validator) error
	SelectValidator(ctx context.Context, chainID, addr string) (*Validator, error)
	ListValidators(ctx context.Context, chainID string) ([]*Validator, error)

	UpsertChain(ctx context.Context, c *Chain) error
	SelectChain(ctx context.Context, id string) (*Chain, error)
	ListChains(ctx context.Context) ([]*Chain, error)
}
