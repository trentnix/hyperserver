package blog

import (
	"context"
	"sync"
)

// cachedValue holds one read-only value, not a map of entries. Changing the key
// replaces that value. Each blog module owns separate feed and metadata caches.
type cachedValue[K comparable, V any] struct {
	mu         sync.Mutex
	generation uint64
	key        K
	value      V
	ready      bool
	loading    chan struct{}
}

// invalidate discards the value after a committed post change.
func (c *cachedValue[K, V]) invalidate() {
	c.mu.Lock()
	c.generation++
	var zero V
	c.value, c.ready = zero, false
	c.mu.Unlock()
}

// load permits one build at a time and caches successful results, including zero
// values. Callers must not modify published values. Waiting requests can cancel
// without cancelling another request's build.
func (c *cachedValue[K, V]) load(ctx context.Context, key K, build func() (V, error)) (V, error) {
	var zero V
	for {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		c.mu.Lock()
		if c.ready && c.key == key {
			value := c.value
			c.mu.Unlock()
			return value, nil
		}
		if c.loading != nil {
			done := c.loading
			c.mu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return zero, ctx.Err()
			}
		}
		c.loading = make(chan struct{})
		c.mu.Unlock()
		return c.rebuild(ctx, key, build)
	}
}

// rebuild retries when invalidation occurs during a build. Errors are not cached.
// Releasing the slot on every exit also wakes waiting requests after a panic.
func (c *cachedValue[K, V]) rebuild(ctx context.Context, key K, build func() (V, error)) (V, error) {
	defer func() {
		c.mu.Lock()
		close(c.loading)
		c.loading = nil
		c.mu.Unlock()
	}()
	var zero V
	for {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		c.mu.Lock()
		generation := c.generation
		c.mu.Unlock()

		value, err := build()
		if err == nil {
			err = ctx.Err()
		}
		if err != nil {
			return zero, err
		}

		c.mu.Lock()
		changed := generation != c.generation
		if !changed {
			c.key, c.value, c.ready = key, value, true
		}
		c.mu.Unlock()
		if !changed {
			return value, nil
		}
	}
}
