package blog

import (
	"container/list"
	"context"
	"database/sql"
	"strings"
	"sync"
)

const (
	searchBodyBytes   = 64 << 20
	searchBodyEntries = 16384
	// Occasionally protect a new entry so unused old bodies can be replaced.
	searchBodyPromotionInterval = 32
)

type searchBodyKey struct{ kind, id string }

type searchBody struct {
	key     searchBodyKey
	version int64
	text    string
}

// searchBodyCache holds derived text, never visibility or search results. A saved
// version change expires an entry. New bodies enter at the eviction end and
// usually move to the front only on a hit. Occasional insertion at the front
// lets the cache adapt to changed content without full-scan churn.
// Its zero value is ready for use.
type searchBodyCache struct {
	mu      sync.Mutex
	entries map[searchBodyKey]*list.Element
	order   list.List
	bytes   int
	inserts uint64
}

func (c *searchBodyCache) get(key searchBodyKey, version int64) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entries[key]
	if e == nil {
		return "", false
	}
	body := e.Value.(searchBody)
	if body.version != version {
		return "", false
	}
	c.order.MoveToFront(e)
	return body.text, true
}

func (c *searchBodyCache) put(body searchBody) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.entries[body.key]; e != nil {
		c.remove(e)
	}
	size := len(body.key.id) + len(body.text)
	if size > searchBodyBytes {
		return
	}
	for c.bytes+size > searchBodyBytes || c.order.Len() >= searchBodyEntries {
		c.remove(c.order.Back())
	}
	if c.entries == nil {
		c.entries = make(map[searchBodyKey]*list.Element)
	}
	e := c.order.PushBack(body)
	c.inserts++
	if c.inserts%searchBodyPromotionInterval == 0 {
		c.order.MoveToFront(e)
	}
	c.entries[body.key] = e
	c.bytes += size
}

func (c *searchBodyCache) remove(e *list.Element) {
	body := e.Value.(searchBody)
	delete(c.entries, body.key)
	c.bytes -= len(body.key.id) + len(body.text)
	c.order.Remove(e)
}

// matchBody reads a missing body in the metadata query's transaction. Comparing
// the saved version makes edits and restores visible across repository instances.
func (s *sqliteRepository) matchBody(ctx context.Context, tx *sql.Tx, match *contentMatcher, kind, id string, version int64) (bool, error) {
	key := searchBodyKey{kind, id}
	text, ok := s.searchBodies.get(key, version)
	if !ok {
		query := "SELECT markdown FROM blog_post_current WHERE id=?"
		if kind == "page" {
			query = "SELECT markdown FROM blog_page_current WHERE id=?"
		}
		if err := tx.QueryRowContext(ctx, query, id).Scan(&text); err != nil {
			return false, err
		}
		text = string(match.normalize(text))
		s.searchBodies.put(searchBody{key: key, version: version, text: text})
	}
	return strings.Contains(text, match.query), nil
}
