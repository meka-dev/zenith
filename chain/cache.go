package chain

import (
	"context"
)

type CachedChain struct {
	Chain

	cache abstractCache[int64, *ValidatorSet]
}

func WithCondCache(chain Chain) Chain {
	return &CachedChain{
		Chain: chain,

		cache: newCondCache[int64, *ValidatorSet](100),
	}
}

func WithRingCache(chain Chain) Chain {
	return &CachedChain{
		Chain: chain,

		cache: newRingCache[int64, *ValidatorSet](100),
	}
}

func (c *CachedChain) ValidatorSet(ctx context.Context, targetHeight int64) (_ *ValidatorSet, err error) {
	return c.cache.Get(ctx, targetHeight, c.Chain.ValidatorSet)
}

type abstractCache[K comparable, V any] interface {
	Len(ctx context.Context) (int, error)
	Get(ctx context.Context, key K, fill func(context.Context, K) (V, error)) (V, error)
}

var (
	_ abstractCache[int64, *ValidatorSet] = (*condCache[int64, *ValidatorSet])(nil)
	_ abstractCache[int64, *ValidatorSet] = (*ringCache[int64, *ValidatorSet])(nil)
)
