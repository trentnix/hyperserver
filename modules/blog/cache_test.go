package blog

import (
	"sync/atomic"
	"testing"
	"testing/synctest"
)

// TestCachedValueConcurrentKeys keeps waiting requests from receiving another key's value.
func TestCachedValueConcurrentKeys(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var cache cachedValue[string, string]
		var builds atomic.Int32
		release := make(chan struct{})
		go func() {
			value, err := cache.load(t.Context(), "first", func() (string, error) {
				builds.Add(1)
				<-release
				return "First", nil
			})
			if err != nil || value != "First" {
				t.Errorf("first request: %q, %v", value, err)
			}
		}()
		synctest.Wait()
		go func() {
			value, err := cache.load(t.Context(), "second", func() (string, error) {
				builds.Add(1)
				return "Second", nil
			})
			if err != nil || value != "Second" {
				t.Errorf("second request: %q, %v", value, err)
			}
		}()
		synctest.Wait()
		if builds.Load() != 1 {
			t.Fatal("different keys started concurrent builds")
		}
		close(release)
		synctest.Wait()
		value, err := cache.load(t.Context(), "second", nil)
		if err != nil || value != "Second" || builds.Load() != 2 {
			t.Fatalf("replacement cache hit: %q, %v, builds=%d", value, err, builds.Load())
		}
	})
}
