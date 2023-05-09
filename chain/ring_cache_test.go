package chain

import (
	"context"
	"errors"
	"math/rand"
	"sync/atomic"
	"testing"
)

func TestRingCacheBasic(t *testing.T) {
	capacity := 5
	c := newRingCache[int, uint64](capacity)
	ctx := context.Background()

	var total, misses uint64

	type result struct {
		k   int
		v   uint64
		err error
	}

	ch := make(chan result, 1000)
	for i := 0; i < cap(ch); i++ {
		k := rand.Intn(capacity)
		go func(k int) {
			v, err := c.Get(ctx, k, func(_ context.Context, k int) (uint64, error) {
				n := atomic.AddUint64(&misses, 1)
				var err error
				if n%3 == 0 {
					err = errors.New("boom")
				}
				return n, err
			})

			atomic.AddUint64(&total, 1)
			ch <- result{k, v, err}
		}(k)
	}

	for i := 0; i < cap(ch); i++ {
		r := <-ch
		t.Logf("result: %+v", r)
	}

	t.Logf("misses, hits: %d, %d", misses, total-misses)
}
