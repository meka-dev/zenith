package store

import (
	"context"
	"fmt"
	"mekapi/trc/eztrc"

	"zenith/metrics"
)

func UpdateMetrics(ctx context.Context, s Store) (err error) {
	chains, err := s.ListChains(ctx)
	if err != nil {
		return fmt.Errorf("list chains: %w", err)
	}

	eztrc.Tracef(ctx, "chain count %d", len(chains))

	for _, c := range chains {
		metrics.ChainInfo.WithLabelValues(c.ID, c.Network, c.PaymentDenom, c.MekatekPaymentAddress).Set(1)

		validators, err := s.ListValidators(ctx, c.ID)
		if err != nil {
			return fmt.Errorf("list validators for %s: %w", c.ID, err)
		}

		eztrc.Tracef(ctx, "%s: validator count %d", c.ID, len(validators))

		for _, v := range validators {
			metrics.ValidatorInfo.WithLabelValues(v.ChainID, v.Address, v.Moniker, v.PaymentAddress).Set(1)
			metrics.ValidatorCreatedAt.WithLabelValues(v.ChainID, v.Address).Set(float64(v.CreatedAt.Unix()))
			metrics.ValidatorUpdatedAt.WithLabelValues(v.ChainID, v.Address).Set(float64(v.UpdatedAt.Unix()))
		}
	}

	return nil
}
