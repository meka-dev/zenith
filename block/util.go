package block

import (
	"context"
	"fmt"
	"strings"
	"time"

	"zenith/chain"
)

var (
	ErrInvalidRequest     = fmt.Errorf("invalid request")
	ErrAuctionUnavailable = fmt.Errorf("auction unavailable")
	ErrAuctionTooOld      = fmt.Errorf("auction too far in the past")
	ErrAuctionTooNew      = fmt.Errorf("auction too far in the future")
	ErrAuctionFinished    = fmt.Errorf("auction already finished")
)

// FixedAllocation is a constant representing the portion of bid payment that
// validators receive. Historically, we started with a dynamic Allocation
// function below, but received pushback from validators, so we have this now.
const FixedAllocation = 0.97

// Allocation is a linear function that computes the validator payment
// allocation for a given auction. It is a function of the total
// participation ratio of validator voting power in the block builder API,
// meant to incentivize validators to convince other validators to join in
// order to increase their (and other validators) profits.
// Output range: [0.5, 0.9]
func Allocation(registeredPower, totalPower int64) float64 {
	const min, max = 0.5, 0.9
	powerShare := float64(registeredPower) / float64(totalPower)
	return powerShare*(max-min) + min
}

func boolString(b bool, ifTrue, ifFalse string) string {
	if b {
		return ifTrue
	}
	return ifFalse
}

func getTxBytes(ctx context.Context, c chain.Chain, tx []byte) (int64, error) {
	transaction, err := c.DecodeTransaction(ctx, tx)
	if err != nil {
		return 0, fmt.Errorf("decode transaction: %w", err)
	}

	nbytes, err := transaction.ByteCount()
	if err != nil {
		return 0, fmt.Errorf("get transaction gas: %w", err)
	}

	return nbytes, nil
}

func getTxGas(ctx context.Context, c chain.Chain, tx []byte) (int64, error) {
	transaction, err := c.DecodeTransaction(ctx, tx)
	if err != nil {
		return 0, fmt.Errorf("decode transaction: %w", err)
	}

	gas, err := transaction.GasAmount()
	if err != nil {
		return 0, fmt.Errorf("get transaction gas: %w", err)
	}

	return gas, nil
}

func traceTime(t time.Time) string {
	switch {
	case t.IsZero():
		return "<zero>"
	default:
		return t.Format(time.RFC3339)
	}
}

func sameAddr(a, b string) bool {
	return strings.EqualFold(a, b)
}
