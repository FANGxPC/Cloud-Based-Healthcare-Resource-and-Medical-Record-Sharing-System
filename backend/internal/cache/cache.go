// Package cache is a tiny in-memory TTL cache, used instead of Redis/ElastiCache (which cost money).
//
// How it works on Lambda: a Lambda container handles one request at a time but is reused for
// many requests in a row. A package-level value survives between those invocations, so caching
// the bed-availability Scan for ~15 s turns a burst of dashboard refreshes into one DynamoDB read
// per container. The trade-off is that data can be up to TTL seconds stale, and each container
// has its own copy. That is acceptable for a dashboard, and the API reports `cachedAt` so the
// UI can show it. Writes that must be exact (bookings) always go straight to DynamoDB.
package cache

import (
	"sync"
	"time"
)

// Value caches a single value of type T for a fixed TTL.
type Value[T any] struct {
	TTL time.Duration
	Now func() time.Time // injectable clock for tests; nil means time.Now

	mu       sync.Mutex
	val      T
	loadedAt time.Time
	valid    bool
}

// Get returns the cached value if it is still fresh. Otherwise it calls load, stores the result
// and returns that. hit reports whether the cache was used. loadedAt is when the data was read.
// Errors are not cached: the next call tries again.
func (c *Value[T]) Get(load func() (T, error)) (val T, loadedAt time.Time, hit bool, err error) {
	c.mu.Lock() // the mutex also stops two goroutines from loading at the same time
	defer c.mu.Unlock()

	now := c.now()
	if c.valid && now.Sub(c.loadedAt) < c.TTL {
		return c.val, c.loadedAt, true, nil
	}

	v, err := load()
	if err != nil {
		var zero T
		return zero, time.Time{}, false, err
	}
	c.val, c.loadedAt, c.valid = v, now, true
	return v, now, false, nil
}

// Invalidate drops the cached value. It is called after a write in the same container,
// so an admin sees their own change straight away.
func (c *Value[T]) Invalidate() {
	c.mu.Lock()
	c.valid = false
	c.mu.Unlock()
}

func (c *Value[T]) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
