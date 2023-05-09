package chain

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"golang.org/x/sync/errgroup"
)

func TestCacheGauntlet(t *testing.T) {
	ctx := context.Background()

	for _, testcase := range []struct {
		name string
		cons func(int) abstractCache[int, int]
	}{
		{"ring", func(n int) abstractCache[int, int] { return newRingCache[int, int](n) }},
		{"cond", func(n int) abstractCache[int, int] { return newCondCache[int, int](n) }},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			t.Run("singleflight", func(t *testing.T) {
				cache := testcase.cons(5)
				requestedKey := 123
				fillcount := uint64(0)
				fillkey := make(chan int, 1)
				fillval := make(chan int, 1)
				fillerr := make(chan error, 1)
				workers := 32
				valc := make(chan int, workers)
				errc := make(chan error, workers)

				fill := func(_ context.Context, key int) (int, error) {
					defer atomic.AddUint64(&fillcount, 1)
					fillkey <- key
					return <-fillval, <-fillerr
				}

				// Spawn a bunch of workers that get the same requestedKey.
				for i := 0; i < workers; i++ {
					go func() {
						if val, err := cache.Get(ctx, requestedKey, fill); err == nil {
							valc <- val
						} else {
							errc <- err
						}
					}()
				}

				// We should get a single fill request for requestedKey.
				key := <-fillkey
				if want, have := requestedKey, key; want != have {
					t.Fatalf("fill key: want %d, have %d", want, have)
				}

				// Every worker should be blocked right now.
				select {
				case <-valc:
					t.Fatalf("invalid recv on valc")
				case <-errc:
					t.Fatalf("invalid recv on errc")
				default:
					// good
				}

				// Tell `fill` what value and error to return.
				responseVal := key * 9
				fillval <- responseVal
				fillerr <- nil

				// That should unblock all the workers.
				vals := map[int]int{}
				for i := 0; i < workers; i++ {
					select {
					case val := <-valc:
						vals[val]++
					case err := <-errc:
						t.Errorf("got error: %v", err)
					}
				}

				// We should have exactly 1 call to `fill`.
				if want, have := uint64(1), fillcount; want != have {
					t.Errorf("fillcount: want %d, have %d", want, have)
				}
				t.Logf("fillcount = %d", fillcount)

				// Every worker got the same key -> should have a single value.
				if want, have := 1, len(vals); want != have {
					t.Errorf("len(vals): want %d, have %d", want, have)
				}
				t.Logf("len(vals) = %d", len(vals))

				// That response value should have `workers` observations.
				if want, have := workers, vals[responseVal]; want != have {
					t.Errorf("vals[%d]: want %d, have %d", responseVal, want, have)
				}
				t.Logf("vals[%d] = %d", responseVal, vals[responseVal])
			})

			t.Run("limit", func(t *testing.T) {
				capacity := 64
				cache := testcase.cons(capacity)
				fill := func(ctx context.Context, key int) (int, error) { return key + 1, nil }
				workerCount := 16
				iterationsPerWorker := 4096

				var eg errgroup.Group
				for i := 1; i <= workerCount; i++ {
					i := i
					eg.Go(func() error {
						for j := 1; j <= iterationsPerWorker; j++ {
							key := i * j
							if _, err := cache.Get(ctx, key, fill); err != nil {
								return fmt.Errorf("worker %d: iteration %d: Get err: %v", i, j, err)
							}
							n, err := cache.Len(ctx)
							if err != nil {
								return fmt.Errorf("worker %d: iteration %d: Len err: %v", i, j, err)
							}
							if n > capacity {
								return fmt.Errorf("worker %d: iteration %d: n=%d capacity=%d", i, j, n, capacity)
							}
						}
						return nil
					})
				}
				if err := eg.Wait(); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}
