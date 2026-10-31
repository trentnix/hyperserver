package blog

import (
	"context"
	"sync"
	"time"
)

const archiveCachePages = 10

// loadArchivePage returns a page plus one lookahead entry from a bounded block.
// The caller validates number before calculating offsets. Cache entries contain
// only public metadata and remain read-only after publication to other requests.
func (m *module) loadArchivePage(ctx context.Context, number int) ([]ArchiveEntry, error) {
	block := (number - 1) / archiveCachePages
	posts, err := m.archivePosts.loadUntil(ctx, block, func() ([]ArchiveEntry, time.Time, error) {
		next, err := m.posts.NextPublication(ctx)
		if err != nil {
			return nil, time.Time{}, err
		}
		posts, err := m.posts.ListArchivePage(ctx, archivePageSize*archiveCachePages+1, block*archivePageSize*archiveCachePages)
		return posts, next, err
	})
	if err != nil {
		return nil, err
	}
	start := min((number-1)%archiveCachePages*archivePageSize, len(posts))
	return posts[start:min(start+archivePageSize+1, len(posts))], nil
}

// cachedValue holds one read-only value, not a map of entries. Changing the key
// replaces that value. Each blog module owns separate feed and metadata caches.
type cachedValue[K comparable, V any] struct {
	mu         sync.Mutex
	generation uint64
	key        K
	value      V
	ready      bool
	loading    chan struct{}
	expires    time.Time
	now        func() time.Time
}

// currentTime permits deterministic expiry tests without a timer or worker.
func (c *cachedValue[K, V]) currentTime() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// invalidate discards the value after a committed content change.
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
	return c.loadUntil(ctx, key, func() (V, time.Time, error) {
		value, err := build()
		return value, time.Time{}, err
	})
}

// loadUntil caches a value only until the publication boundary returned by build.
// The builder must read that boundary before reading content. If publication
// occurs during the build, rebuild discards the result and retries.
func (c *cachedValue[K, V]) loadUntil(ctx context.Context, key K, build func() (V, time.Time, error)) (V, error) {
	var zero V
	for {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		c.mu.Lock()
		if c.ready && c.key == key && (c.expires.IsZero() || c.currentTime().Before(c.expires)) {
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
func (c *cachedValue[K, V]) rebuild(ctx context.Context, key K, build func() (V, time.Time, error)) (V, error) {
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

		value, expires, err := build()
		if err == nil {
			err = ctx.Err()
		}
		if err != nil {
			return zero, err
		}

		c.mu.Lock()
		changed := generation != c.generation || (!expires.IsZero() && !c.currentTime().Before(expires))
		if !changed {
			c.key, c.value, c.ready = key, value, true
			c.expires = expires
		}
		c.mu.Unlock()
		if !changed {
			return value, nil
		}
	}
}
