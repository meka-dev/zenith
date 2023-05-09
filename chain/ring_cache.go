package chain

import (
	"context"
	"sync"
)

//
//
//

type ringCache[K comparable, V any] struct {
	mu    sync.Mutex
	index map[K]int // k -> pos in ring
	ring  []*ringItem[K, V]
	head  int
}

type ringItem[K comparable, V any] struct {
	mu    sync.RWMutex
	key   K
	value V
	err   error
	ok    bool
}

func newRingCache[K comparable, V any](capacity int) *ringCache[K, V] {
	return &ringCache[K, V]{
		index: make(map[K]int, capacity),
		ring:  make([]*ringItem[K, V], capacity),
	}
}

func (c *ringCache[K, V]) Len(ctx context.Context) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.index), nil
}

func (c *ringCache[K, V]) Get(ctx context.Context, k K, fill func(context.Context, K) (V, error)) (V, error) {
	it := c.item(k)

	it.mu.RLock()
	v, err, ok := it.value, it.err, it.ok
	it.mu.RUnlock()

	if ok {
		return v, err
	}

	it.mu.Lock()
	v, err, ok = it.value, it.err, it.ok // check if someone beat us to the punch
	if ok {
		it.mu.Unlock()
		return v, err
	}

	it.value, it.err = fill(ctx, k)
	it.ok = true

	v, err = it.value, it.err
	it.mu.Unlock()

	if err != nil {
		c.del(k)
	}

	return v, err
}

func (c *ringCache[K, V]) item(k K) *ringItem[K, V] {
	c.mu.Lock()
	defer c.mu.Unlock()

	pos, ok := c.index[k]
	if ok {
		return c.ring[pos]
	}

	if old := c.ring[c.head]; old != nil {
		delete(c.index, old.key)
	}

	it := &ringItem[K, V]{key: k}
	c.index[k] = c.head
	c.ring[c.head] = it
	c.head = (c.head + 1) % len(c.ring)

	return it
}

func (c *ringCache[K, V]) del(k K) {
	c.mu.Lock()
	defer c.mu.Unlock()

	pos, ok := c.index[k]
	if !ok {
		return
	}

	c.ring[pos] = nil
	delete(c.index, k)
}
