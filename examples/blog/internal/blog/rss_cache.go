package blog

import (
	"context"
	"crypto/sha256"
	"fmt"
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
	return c.cachedValue.load(ctx, origin, func() (*feedDocument, error) {
		data, err := build()
		if err != nil {
			return nil, err
		}
		return &feedDocument{data: data, etag: fmt.Sprintf(`"%x"`, sha256.Sum256(data))}, nil
	})
}
