package chain

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func getEnv[V any](t *testing.T, key string, parse func(string) (V, error), def V) (res V) {
	t.Helper()
	defer func() { t.Logf("%s=%v", key, res) }()
	if val := os.Getenv(key); val != "" {
		if v, err := parse(val); err == nil {
			return v
		}
	}
	return def
}

func TestCondCacheBasic(t *testing.T) {
	seed := getEnv(t, "SEED", func(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) }, time.Now().UnixNano())
	ctx := context.Background()
	capacity := 5
	cache := newCondCache[int, uint64](capacity)

	var total, misses uint64

	fill := func(_ context.Context, k int) (uint64, error) {
		n := atomic.AddUint64(&misses, 1)
		var err error
		if n%3 == 0 {
			err = errors.New("boom")
		}
		return n, err
	}

	type result struct {
		k   int
		v   uint64
		err error
	}

	resultc := make(chan result, 1000)
	rng := rand.New(rand.NewSource(seed))

	for i := 0; i < cap(resultc); i++ {
		k := rng.Intn(capacity)
		go func(k int) {
			v, err := cache.Get(ctx, k, fill)
			atomic.AddUint64(&total, 1)
			resultc <- result{k, v, err}
		}(k)
	}

	for i := 0; i < cap(resultc); i++ {
		r := <-resultc
		t.Logf("result: %+v", r)
	}

	t.Logf("total=%d misses=%d hits=%d", total, misses, total-misses)
}
