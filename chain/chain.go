package chain

import (
	"context"
	"errors"
)

var (
	ErrInvalidKey   = errors.New("invalid key")
	ErrBadSignature = errors.New("bad signature")
	ErrNoPayment    = errors.New("no payment")
)

type Chain interface {
	ID() string
	ValidatePaymentAddress(ctx context.Context, addr string) error
	VerifySignature(ctx context.Context, pubKeyType string, pubKeyBytes []byte, msg []byte, sig []byte) error
	LatestHeight(ctx context.Context) (int64, error)
	DecodeTransaction(ctx context.Context, txb []byte) (Transaction, error)
	EncodeTransaction(ctx context.Context, tx Transaction) ([]byte, error)
	AccountBalance(ctx context.Context, height int64, addr, denom string) (int64, error)
	ValidatorSet(ctx context.Context, height int64) (*ValidatorSet, error)
	PredictProposer(ctx context.Context, valset *ValidatorSet, height int64) (*Validator, error)
	GetPayment(ctx context.Context, msg Message, denom string) (src, dst string, amount int64, err error)
}

type Transaction interface {
	Messages() []Message
	ByteCount() (int64, error)
	GasAmount() (int64, error)
}

type Message interface {
	// the only thing we do with a Message is type-assert it to something else
}

type ValidatorSet struct {
	Height     int64
	Validators []*Validator
	Set        map[string]*Validator
	TotalPower int64
}

type Validator struct {
	Address          string
	Moniker          string
	PaymentAddress   string
	PubKeyType       string
	PubKeyBytes      []byte
	VotingPower      int64
	ProposerPriority int64 // <-- at the original height
}
