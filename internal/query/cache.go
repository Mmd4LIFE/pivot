package query

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
	"github.com/Mmd4LIFE/pivot/internal/policy"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

// The cache statuses written to the query log. A query is in exactly one of
// these states, and between them they account for every execution.
const (
	// CacheHit was served from cache. No connector was opened.
	CacheHit = "hit"

	// CacheMiss ran against the source and its result was kept.
	CacheMiss = "miss"

	// CacheUncached ran against the source and was not kept -- because the
	// cache is off, because the caller could not be fingerprinted, or because
	// the result outgrew the per-entry budget.
	CacheUncached = "uncached"
)

// Defaults for the L1 cache. Modest on purpose: ADR-0003 targets a 512 MB
// container, and a cache that is the reason an instance is killed has made
// things worse in exchange for being fast when it works.
const (
	// DefaultMaxBytes is what the whole cache may hold.
	DefaultMaxBytes int64 = 64 << 20

	// DefaultMaxEntryBytes is what one result may hold. Deliberately a small
	// fraction of the total: a cache whose every slot can be filled by one
	// answer is a cache with one entry in it.
	DefaultMaxEntryBytes int64 = 8 << 20

	// DefaultTTL bounds how long a result is trusted without anything telling
	// the cache it changed. Generation bumps handle the changes Pivot knows
	// about; this is the backstop for the ones it does not -- somebody loading
	// a table behind its back, which is the ordinary case for a warehouse.
	DefaultTTL = 60 * time.Second
)

/*
Cache is the L1 result cache: in process, bounded by bytes, keyed on the
caller's policy set.

It holds decoded rows rather than Arrow batches. [ADR-0012] has the argument;
the short version is that an entry exists to be shared and `arrow.RecordBatch`
is reference counted, so sharing one safely is a discipline rather than a
property -- and that caching rows keeps a hit and a miss on one conversion path
so the two cannot drift.

Safe for concurrent use. Entries are immutable once stored: a replay hands out
the stored rows without copying them, which is only sound because nothing ever
writes to them again.
*/
type Cache struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	order   *list.List

	bytes         int64
	maxBytes      int64
	maxEntryBytes int64

	ttl time.Duration
	now func() time.Time

	/*
		generations is the invalidation mechanism ADR-0006 requires: a counter
		per connection, folded into every key derived from it.

		Bumping one makes every entry for that connection unreachable with a
		single increment -- no scan, no reverse index, no delete that can miss
		an entry it should have cleared. The unreachable entries are not
		removed; they age out by TTL and are evicted by the byte bound, which
		is the trade ADR-0006 names explicitly.
	*/
	generations map[uuid.UUID]uint64

	instruments *Instruments

	stats Stats
}

// Stats is what the cache has done. Counters only -- the gauges a dashboard
// wants are derived from Entries and Bytes, which are read under the lock.
type Stats struct {
	Hits      uint64
	Misses    uint64
	Uncached  uint64
	Evictions uint64
	Expired   uint64
	Entries   int
	Bytes     int64
}

// entry is one cached result. Immutable after it is stored.
type entry struct {
	key string

	columns   []connectors.Column
	rows      [][]any
	truncated bool

	bytes   int64
	expires time.Time
}

// CacheOption configures a Cache.
type CacheOption func(*Cache)

// WithMaxBytes bounds the whole cache.
func WithMaxBytes(n int64) CacheOption {
	return func(c *Cache) {
		if n > 0 {
			c.maxBytes = n
		}
	}
}

// WithMaxEntryBytes bounds one result.
func WithMaxEntryBytes(n int64) CacheOption {
	return func(c *Cache) {
		if n > 0 {
			c.maxEntryBytes = n
		}
	}
}

// WithTTL bounds how long an entry is trusted.
func WithTTL(d time.Duration) CacheOption {
	return func(c *Cache) {
		if d > 0 {
			c.ttl = d
		}
	}
}

// withCacheClock replaces the time source, so a test can expire an entry
// without sleeping. Unexported: a caller who could move the clock could hold a
// stale result past its TTL.
func withCacheClock(now func() time.Time) CacheOption {
	return func(c *Cache) { c.now = now }
}

// NewCache builds an empty cache.
func NewCache(opts ...CacheOption) *Cache {
	c := &Cache{
		entries:       make(map[string]*list.Element),
		order:         list.New(),
		maxBytes:      DefaultMaxBytes,
		maxEntryBytes: DefaultMaxEntryBytes,
		ttl:           DefaultTTL,
		now:           time.Now,
		generations:   make(map[uuid.UUID]uint64),
		instruments:   newCacheInstruments(),
	}

	for _, opt := range opts {
		opt(c)
	}

	// Registered here rather than by whoever builds the server, so that a
	// cache which exists is a cache that can be observed. A gauge somebody
	// has to remember to wire up is a gauge that reads zero in the one
	// deployment where the question mattered.
	//
	// The error is dropped deliberately: it means this meter already has
	// these instruments, which happens when a process builds a second cache,
	// and refusing to build one over it would trade the feature for its
	// measurement.
	c.observe()

	return c
}

/*
Key identifies a result.

The shape is ADR-0006's: the statement, the tenant, the caller's resolved
policy set, and the generation counters of everything it derives from. The
fingerprint is not an extra check performed alongside the lookup -- it is part
of the key, so two callers with different policy sets are not *refused* each
other's entry, they are structurally unable to name it.

Returns false for a caller with no resolved fingerprint. There is deliberately
no way to build a key without one.
*/
func (c *Cache) Key(
	fp policy.Fingerprint, orgID, connectionID uuid.UUID, statement string, maxRows int64,
) (string, bool) {
	if !fp.Resolved() {
		return "", false
	}

	var b strings.Builder

	// Every element is length-prefixed. Without that, a connection id and a
	// statement can be rearranged into the same byte sequence as a different
	// pair -- and two different questions hashing alike is the one failure
	// that matters here.
	b.WriteString("query/v1")
	writeKeyPart(&b, orgID.String())
	writeKeyPart(&b, connectionID.String())
	writeKeyPart(&b, fp.String())
	writeKeyPart(&b, fmt.Sprintf("%d", maxRows))
	writeKeyPart(&b, fmt.Sprintf("%d", c.Generation(connectionID)))
	writeKeyPart(&b, statement)

	sum := sha256.Sum256([]byte(b.String()))

	return hex.EncodeToString(sum[:]), true
}

func writeKeyPart(b *strings.Builder, part string) {
	fmt.Fprintf(b, "|%d:%s", len(part), part)
}

// Generation returns the current counter for a connection.
func (c *Cache) Generation(connectionID uuid.UUID) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.generations[connectionID]
}

/*
Invalidate bumps a connection's generation, making every result derived from it
unreachable.

One increment rather than a sweep. The alternative needs an index from
connections back to keys, and a keyspace big enough to be worth caching is one
where that index is the expensive part -- see ADR-0006.
*/
func (c *Cache) Invalidate(connectionID uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.generations[connectionID]++
}

// Get returns a cached result, or false.
func (c *Cache) Get(ctx context.Context, key string) (connectors.Stream, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	element, ok := c.entries[key]
	if !ok {
		c.stats.Misses++
		c.instruments.recordLookup(ctx, CacheMiss)

		return nil, false
	}

	found, ok := element.Value.(*entry)
	if !ok {
		return nil, false
	}

	if c.now().After(found.expires) {
		c.removeLocked(element)
		c.stats.Expired++
		c.stats.Misses++
		c.instruments.recordLookup(ctx, CacheMiss)

		return nil, false
	}

	c.order.MoveToFront(element)
	c.stats.Hits++
	c.instruments.recordLookup(ctx, CacheHit)

	return &replay{entry: found}, true
}

/*
Put stores a result, evicting by least-recent use until it fits.

A result larger than the per-entry budget is refused rather than trimmed. A
trimmed entry is a partial answer that would be served as a whole one, which is
the same failure Part 16 refused for truncated results -- and unlike a
truncated result there would be no flag on it to say so.
*/
func (c *Cache) Put(
	ctx context.Context, key string, columns []connectors.Column,
	rows [][]any, truncated bool, size int64,
) bool {
	if size > c.maxEntryBytes || size > c.maxBytes {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if element, ok := c.entries[key]; ok {
		c.removeLocked(element)
	}

	stored := &entry{
		key:       key,
		columns:   columns,
		rows:      rows,
		truncated: truncated,
		bytes:     size,
		expires:   c.now().Add(c.ttl),
	}

	c.entries[key] = c.order.PushFront(stored)
	c.bytes += size

	var evicted int64

	for c.bytes > c.maxBytes {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}

		c.removeLocked(oldest)
		c.stats.Evictions++
		evicted++
	}

	c.instruments.recordEviction(ctx, evicted)

	return true
}

// MaxEntryBytes is the per-result budget, which the pipeline needs in order to
// stop teeing a result that has outgrown it.
func (c *Cache) MaxEntryBytes() int64 { return c.maxEntryBytes }

// Stats reports what the cache has done and how much it is holding.
func (c *Cache) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := c.stats
	out.Entries = len(c.entries)
	out.Bytes = c.bytes

	return out
}

// countUncached records a query that was not eligible to be cached, so the
// hit rate is a fraction of the queries the cache could have helped rather
// than of every query that ever ran.
func (c *Cache) countUncached(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.stats.Uncached++
	c.instruments.recordLookup(ctx, CacheUncached)
}

func (c *Cache) removeLocked(element *list.Element) {
	found, ok := element.Value.(*entry)
	if !ok {
		return
	}

	c.order.Remove(element)
	delete(c.entries, found.key)

	c.bytes -= found.bytes
}

/*
replay serves a cached result as a [connectors.Stream].

A Stream and not a new type, so that everything downstream -- the Arrow
conversion, the row and byte counting, the query log -- runs on a cache hit
exactly as it does on a miss. The point is not tidiness: it is that a hit and a
miss cannot produce different answers if there is only one path from here on.

It holds the entry's rows without copying them. Sound because an entry is never
written again after it is stored, and because [connectors.Stream] already says
a row is valid only until the next Next.
*/
type replay struct {
	entry *entry
	index int
	done  bool

	/*
		cells is this reader's own copy of the current row.

		[connectors.Stream] says a row is valid only until the next Next and
		that a caller who keeps one copies it -- so a caller that writes to the
		slice it was handed is already misbehaving. The difference here is what
		its misbehavior costs: a driver-backed stream owns a buffer per stream
		and corrupts only itself, while this one would be writing into an entry
		every other reader is still being served from.

		So the copy is not defending the caller from itself. It is keeping one
		caller's bug out of everybody else's results.
	*/
	cells []any
}

func (r *replay) Columns() []connectors.Column { return r.entry.columns }

func (r *replay) Next() bool {
	if r.done || r.index >= len(r.entry.rows) {
		return false
	}

	row := r.entry.rows[r.index]

	if cap(r.cells) < len(row) {
		r.cells = make([]any, len(row))
	}

	r.cells = r.cells[:len(row)]
	copy(r.cells, row)

	r.index++

	return true
}

func (r *replay) Row() []any {
	if r.index == 0 {
		return nil
	}

	return r.cells
}

// Err is always nil. A cached result is one that already succeeded; there is
// no failure left to report.
func (r *replay) Err() error { return nil }

// Truncated carries the flag the original result had. A result cut at the row
// cap is still the right answer to the question that asked for that cap, and
// serving it without the flag would turn a signaled partial answer into a
// silent one.
func (r *replay) Truncated() bool { return r.entry.truncated }

func (r *replay) Close() error {
	r.done = true

	return nil
}

/*
Watch subscribes the cache to the repository's change events, so that editing a
connection makes everything cached from it unreachable.

The bus is the right place for this rather than a call inside each repository
method: the events already exist, already carry the entity and the tenant, and
are already how the audit log learns the same facts. A cache that had to be
invalidated by hand at every write site is a cache that is correct until
somebody adds the write site that forgets.

Only connections are watched. A user or a role changing does not stale a
result -- it changes who may *see* one, and that is already handled, because
their grants change and so does the fingerprint in their key. This is the
distinction worth keeping straight: generation counters answer "is this result
still true", and the fingerprint answers "may this caller have it".
*/
func (c *Cache) Watch(bus *repo.EventBus) {
	if bus == nil {
		return
	}

	bus.Subscribe(func(_ context.Context, e repo.ChangeEvent) {
		if e.EntityType != repo.EntityConnection {
			return
		}

		c.Invalidate(e.EntityID)
	})
}
