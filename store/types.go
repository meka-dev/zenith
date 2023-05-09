package store

import (
	"errors"
	"strings"
	"time"

	"github.com/gofrs/uuid"
)

type Chain struct {
	ID                    string
	Network               string
	PaymentDenom          string
	MekatekPaymentAddress string
	Timeout               time.Duration
	NodeURIs              []string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type Auction struct {
	ChainID                 string
	Height                  int64
	ValidatorAddress        string
	ValidatorAllocation     float64
	ValidatorPaymentAddress string
	MekatekPaymentAddress   string
	PaymentDenom            string
	RegisteredPower         int64
	TotalPower              int64
	CreatedAt               time.Time
	FinishedAt              time.Time // the only field that can be user-modified after creation
}

type Bid struct {
	ID               uuid.UUID
	ChainID          string
	Height           int64
	Kind             BidKind
	Txs              [][]byte
	Priority         int64
	MekatekPayment   int64
	ValidatorPayment int64
	Payments         []Payment
	State            BidState // the only field that can be user-modified after creation
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type Payment struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Amount int64  `json:"amount"`
}

type BidState string

const (
	BidStatePending  BidState = "pending"
	BidStateAccepted BidState = "accepted"
	BidStateRejected BidState = "rejected"
)

func ParseBidState(s string) BidState {
	switch strings.ToLower(s) {
	case string(BidStateAccepted):
		return BidStateAccepted
	case string(BidStateRejected):
		return BidStateRejected
	default:
		return BidStatePending
	}
}

type BidKind string

const (
	BidKindTop   BidKind = "top"
	BidKindBlock BidKind = "block"
)

func ParseBidKind(s string) BidKind {
	switch strings.ToLower(s) {
	case string(BidKindBlock):
		return BidKindBlock
	default:
		return BidKindTop
	}
}

type Challenge struct {
	ID               uuid.UUID
	ChainID          string
	ValidatorAddress string
	PubKeyBytes      []byte
	PubKeyType       string
	PaymentAddress   string
	Challenge        []byte
	CreatedAt        time.Time
}

type Validator struct {
	ChainID        string
	Address        string
	Moniker        string
	PubKeyBytes    []byte
	PubKeyType     string
	PaymentAddress string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

var ErrNotFound = errors.New("not found")
