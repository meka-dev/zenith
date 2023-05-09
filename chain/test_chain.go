package chain

import (
	"context"
)

type TestChain struct {
	ChainID           string
	Height            int64
	Validators        ValidatorSet
	PredictedProposer Validator
}

var _ Chain = (*TestChain)(nil)

func (c *TestChain) ID() string {
	return c.ChainID
}

func (c *TestChain) VerifySignature(ctx context.Context, pubKeyType string, pubKeyBytes []byte, msg []byte, sig []byte) error {
	return nil
}

func (c *TestChain) ValidatePaymentAddress(ctx context.Context, addr string) error {
	return nil
}

func (c *TestChain) DecodeTransaction(ctx context.Context, txb []byte) (Transaction, error) {
	return &TestTransaction{s: string(txb)}, nil
}

func (c *TestChain) EncodeTransaction(ctx context.Context, tx Transaction) ([]byte, error) {
	return []byte(tx.(*TestTransaction).s), nil
}

func (c *TestChain) AccountBalance(ctx context.Context, height int64, addr, denom string) (int64, error) {
	return 100, nil
}

func (c *TestChain) LatestHeight(ctx context.Context) (int64, error) {
	return c.Height, nil
}

func (c *TestChain) ValidatorSet(ctx context.Context, _ int64) (*ValidatorSet, error) {
	return &c.Validators, nil
}

func (c *TestChain) PredictProposer(ctx context.Context, valset *ValidatorSet, height int64) (*Validator, error) {
	return &c.PredictedProposer, nil
}

func (c *TestChain) GetPayment(ctx context.Context, msg Message, denom string) (src, dst string, amount int64, err error) {
	return "", "", 0, ErrNoPayment
}

type TestTransaction struct {
	s string
}

func (t *TestTransaction) Messages() []Message       { return []Message{Message(t.s)} }
func (t *TestTransaction) ByteCount() (int64, error) { return 1024, nil }
func (t *TestTransaction) GasAmount() (int64, error) { return 10, nil }
