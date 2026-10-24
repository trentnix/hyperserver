package blog

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
)

// feedDocument is immutable after publication. The origin identifies its public URLs.
type feedDocument struct {
	origin string
	data   []byte
	etag   string
}

// feedCache retains one complete feed per module and permits one build at a time.
// A generation change prevents a concurrent build from undoing invalidation.
type feedCache struct {
	mu         sync.Mutex
	generation uint64
	document   *feedDocument
	building   chan struct{}
}

// invalidate drops the cached document after a committed post change.
func (c *feedCache) invalidate() {
	c.mu.Lock()
	c.generation++
	c.document = nil
	c.mu.Unlock()
}

// load reuses completed XML or builds it once. Waiting requests can cancel.
// Failed builds are not cached, and an invalidated build retries against storage.
func (c *feedCache) load(ctx context.Context, origin string, build func() ([]byte, error)) (*feedDocument, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.mu.Lock()
		if c.document != nil && c.document.origin == origin {
			document := c.document
			c.mu.Unlock()
			return document, nil
		}
		if c.building != nil {
			done := c.building
			c.mu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		c.building = make(chan struct{})
		c.mu.Unlock()
		return c.rebuild(ctx, origin, build)
	}
}

// rebuild owns the build slot until it publishes current XML or fails.
// The deferred release also frees waiting readers if rendering panics.
func (c *feedCache) rebuild(ctx context.Context, origin string, build func() ([]byte, error)) (*feedDocument, error) {
	defer func() {
		c.mu.Lock()
		close(c.building)
		c.building = nil
		c.mu.Unlock()
	}()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.mu.Lock()
		generation := c.generation
		c.mu.Unlock()

		data, err := build()
		if err == nil {
			err = ctx.Err()
		}
		if err != nil {
			return nil, err
		}
		document := &feedDocument{origin: origin, data: data, etag: fmt.Sprintf(`"%x"`, sha256.Sum256(data))}

		c.mu.Lock()
		changed := generation != c.generation
		if !changed {
			c.document = document
		}
		c.mu.Unlock()
		if !changed {
			return document, nil
		}
	}
}
