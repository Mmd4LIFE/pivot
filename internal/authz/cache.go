package authz

import (
	"context"
	"sync"
	"time"
)

// CacheTTL bounds how long a cached decision is trusted.
//
// Three seconds, against a requirement that permission changes take effect
// within five. The requirement is met twice over: a write through [Cache.Write]
// invalidates immediately, and the TTL is the backstop for the case where the
// write did not happen here — another node in a Phase 9 deployment, an
// administrator editing the database directly, a future background job.
//
// Deriving the guarantee from a clock rather than from invalidation being
// correct is the point. Invalidation logic is exactly the kind that is right
// until someone adds a code path that forgets it, and a stale *allow* is a
// security failure rather than a stale page.
const CacheTTL = 3 * time.Second

// cacheEntry is one remembered decision.
type cacheEntry struct {
	decision Decision
	expires  time.Time
}

// Cache memoizes decisions in process.
//
// In-process is the right scope for a single binary and explicitly wrong for
// multi-node, where N replicas each keep their own copy — which the TTL bounds
// rather than solves. Phase 9 moves this to Valkey; ADR-0009 already names that.
type Cache struct {
	inner Checker
	now   func() time.Time
	ttl   time.Duration

	mu      sync.RWMutex
	entries map[string]cacheEntry

	// generation is bumped on every write. It is part of the key, so a write
	// invalidates every prior entry at once without walking the map — and
	// without the risk of a targeted invalidation missing an entry it should
	// have cleared.
	generation uint64

	hits, misses uint64
}

// NewCache wraps a checker with in-process memoization.
func NewCache(inner Checker) *Cache {
	return &Cache{
		inner:   inner,
		now:     time.Now,
		ttl:     CacheTTL,
		entries: make(map[string]cacheEntry),
	}
}

// SetClock replaces the time source. For tests only.
func (c *Cache) SetClock(now func() time.Time) { c.now = now }

// Check answers from the cache when it can, and from the inner checker when it
// cannot.
//
// A failed inner check is never cached. Caching an error would turn one
// database blip into three seconds of denials for everyone.
func (c *Cache) Check(ctx context.Context, req Request) (Decision, error) {
	key, generation := c.key(req)

	if decision, ok := c.lookup(key); ok {
		return decision, nil
	}

	decision, err := c.inner.Check(ctx, req)
	if err != nil {
		return Decision{}, err
	}

	c.store(key, generation, decision)

	return decision, nil
}

// Explain always goes to the inner checker.
//
// An explanation is for a human asking "why?", so it must reflect the graph as
// it is now, not as it was up to three seconds ago. It is also not on any hot
// path, so there is nothing to gain by caching it.
func (c *Cache) Explain(ctx context.Context, req Request) (Explanation, error) {
	return c.inner.Explain(ctx, req)
}

// Invalidate drops every cached decision.
//
// Called after any relationship write. Wholesale rather than targeted because a
// single tuple can change decisions for every member of a nested group, and
// working out exactly which is the kind of cleverness that is subtly wrong for
// a year. The cache refills in microseconds.
func (c *Cache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.generation++
	c.entries = make(map[string]cacheEntry)
}

// key renders a cache key and reads the current generation under one lock.
func (c *Cache) key(req Request) (string, uint64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return req.String(), c.generation
}

// lookup returns a live cached decision.
func (c *Cache) lookup(key string) (Decision, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok || !entry.expires.After(c.now()) {
		c.misses++

		return Decision{}, false
	}

	c.hits++

	return entry.decision, true
}

// store records a decision, unless a write landed while it was being computed.
func (c *Cache) store(key string, generation uint64, decision Decision) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// The generation captured before the inner check no longer matches, so a
	// relationship changed while this decision was in flight. It may already be
	// wrong, and storing it would give it a full TTL of life.
	if generation != c.generation {
		return
	}

	c.entries[key] = cacheEntry{decision: decision, expires: c.now().Add(c.ttl)}
}

// Stats reports hits, misses and size, for tests and for Part 14's metrics.
func (c *Cache) Stats() (hits, misses uint64, size int) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.hits, c.misses, len(c.entries)
}
