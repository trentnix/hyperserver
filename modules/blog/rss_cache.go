package blog

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"
)

// feedDocument is immutable after publication.
type feedDocument struct {
	data []byte
	etag string
}

// feedCache retains one complete feed, keyed by the origin of its public URLs.
type feedCache struct {
	cachedValue[string, *feedDocument]
}

// load adds the feed's ETag before publishing its XML through the shared cache.
func (c *feedCache) load(ctx context.Context, origin string, build func() ([]byte, error)) (*feedDocument, error) {
	return c.loadUntil(ctx, origin, func() ([]byte, time.Time, error) {
		data, err := build()
		return data, time.Time{}, err
	})
}

// loadUntil expires a feed when its next scheduled post becomes public.
func (c *feedCache) loadUntil(ctx context.Context, origin string, build func() ([]byte, time.Time, error)) (*feedDocument, error) {
	return c.cachedValue.loadUntil(ctx, origin, func() (*feedDocument, time.Time, error) {
		data, expires, err := build()
		if err != nil {
			return nil, time.Time{}, err
		}
		return &feedDocument{data: data, etag: fmt.Sprintf(`"%x"`, sha256.Sum256(data))}, expires, nil
	})
}
