package chain

import (
	"context"
	"fmt"
	"sync"
)

type condCache[K comparable, V any] struct {
	mtx   sync.Mutex
	limit int
	cache map[K]*condItem[V]
	order seq[K]
}

func newCondCache[K comparable, V any](limit int) *condCache[K, V] {
	return &condCache[K, V]{
		limit: limit,
		cache: map[K]*condItem[V]{},
		order: seq[K]{max: limit},
	}
}

func (c *condCache[K, V]) Len(ctx context.Context) (int, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return len(c.cache), nil
}

func (c *condCache[K, V]) Get(ctx context.Context, key K, fill func(context.Context, K) (V, error)) (V, error) {
	// Get the item from the cache, or create an empty item if it doesn't exist.
	item, valid := c.get(key)

	// If we got an existing item, we can return it directly.
	if valid {
		return item.get()
	}

	// Otherwise, the item is new, and we have to fill it.
	val, err := fill(ctx, key)
	if err != nil {
		// If the fill failed, the item is defunct, and it should be removed.
		// The next caller to request `key` will find it missing from the cache,
		// and try the fill again. But any caller waiting on this specific item
		// should be un-blocked with the error, which happens below.
		c.del(key)
	}

	// Notify waiting readers that the item has a result (which may be an error).
	item.set(val, err)

	// And notify our caller, too.
	return val, err
}

// get takes the lock and returns the item corresponding to the key. If the
// caller can use the item directly, the returned bool is true. If the caller
// needs to fill the returned item via `set`, the returned bool is false.
//
// get also makes sure that the cache is no bigger than the limit provided at
// construction, removing the least-recently accessed keys if it gets too big.
func (c *condCache[K, V]) get(key K) (*condItem[V], bool) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	defer func() {
		// `key` is the most-recently accessed key, so we poke it up to the
		// front of the line. The return value tells us any old keys that we
		// need to remove.
		for _, kill := range c.order.poke(key) {
			delete(c.cache, kill)
		}
	}()

	// If there's already an item for the key, great! Return it, and tell our
	// caller it's valid, so they don't need to fill it.
	item, ok := c.cache[key]
	if ok {
		return item, true
	}

	// No item yet. Create a fresh item, stick it in the cache, and return it to
	// our caller, but tell them it's not valid, so they will fill it.
	item = newCondItem[V]()
	c.cache[key] = item
	return item, false
}

// del takes the lock and removes the item corresponding to key.
func (c *condCache[K, V]) del(key K) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	// If there was an item for this key, and for whatever reason it hadn't been
	// set yet, we want to tell any waiting readers that it's hopeless and they
	// should give up.
	if item, ok := c.cache[key]; ok {
		var zeroval V
		item.set(zeroval, fmt.Errorf("deleted")) // if item was already set, this is a no-op
	}

	// In any case, delete it from the cache and order.
	delete(c.cache, key)
	c.order.drop(key)
}

//
//
//

type condItem[T any] struct {
	mtx   sync.Mutex
	cond  sync.Cond
	val   T
	err   error
	ready bool
}

// newCondItem creates a non-ready item that can be added to a cache.
func newCondItem[T any]() *condItem[T] {
	x := &condItem[T]{}
	x.cond.L = &x.mtx
	return x
}

// get returns the item's value and error. If the item hasn't yet received a
// call to set, it will block until that happens.
func (x *condItem[T]) get() (T, error) {
	x.mtx.Lock()
	defer x.mtx.Unlock()
	for !x.ready {
		x.cond.Wait() // wait for another goroutine to `set`
	}
	return x.val, x.err
}

// set the item's value and error to val and err, respectively, and notify all
// waiting getters that the item has been updated. If set was already called on
// this item, then subsequent calls are no-ops.
func (x *condItem[T]) set(val T, err error) {
	x.mtx.Lock()
	defer x.mtx.Unlock()
	if !x.ready {
		x.val = val
		x.err = err
		x.ready = true
		x.cond.Broadcast() // wake up all the goroutines waiting in `get`
	}
}

//
//
//

type seq[K comparable] struct {
	set []K
	max int
}

// poke moves `key` up to the front of the line. If that makes the line too
// long, it removes the least-recently used keys from the back of the line, and
// returns them to the caller, to signal that the caller should delete those
// keys, too.
func (s *seq[K]) poke(key K) (remove []K) {
	defer func() {
		if d := len(s.set) - s.max; d > 0 {
			remove = s.set[:d] // LRU keys should be removed
			s.set = s.set[d:]  // others remain
		}
	}()

	for i, k := range s.set {
		if k == key {
			s.set = append(s.set[:i], append(s.set[i+1:], key)...)
			return
		}
	}

	s.set = append(s.set, key)
	return
}

// drop the key out of the line, if it's in the line.
func (s *seq[K]) drop(key K) {
	for i, k := range s.set {
		if k == key {
			s.set = append(s.set[:i], s.set[i+1:]...)
		}
	}
}
