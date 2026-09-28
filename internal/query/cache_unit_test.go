package query

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/connectors"
	"github.com/Mmd4LIFE/pivot/internal/datatype"
	"github.com/Mmd4LIFE/pivot/internal/policy"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

/*
The cache itself, without a pipeline around it.

The pipeline tests prove the cache does the right thing in place. These prove
the key is made of what it claims to be made of, one component at a time --
which is the kind of thing that is easy to get right today and easy to break
with a refactor that looks harmless.
*/

// Every component of the key changes the key.
func TestEveryKeyComponentMatters(t *testing.T) {
	t.Parallel()

	var (
		cache = NewCache()
		org   = uuid.New()
		conn  = uuid.New()
		fp    = fingerprintFor(t, authz.RelationAnalyst)
	)

	base, ok := cache.Key(fp, org, conn, "SELECT 1", 100)
	if !ok {
		t.Fatal("no key for a resolved fingerprint")
	}

	cases := map[string]func() (string, bool){
		"a different policy set": func() (string, bool) {
			return cache.Key(fingerprintFor(t, authz.RelationAdmin), org, conn, "SELECT 1", 100)
		},
		"a different tenant": func() (string, bool) {
			return cache.Key(fp, uuid.New(), conn, "SELECT 1", 100)
		},
		"a different connection": func() (string, bool) {
			return cache.Key(fp, org, uuid.New(), "SELECT 1", 100)
		},
		"a different statement": func() (string, bool) {
			return cache.Key(fp, org, conn, "SELECT 2", 100)
		},
		"a different row cap": func() (string, bool) {
			return cache.Key(fp, org, conn, "SELECT 1", 50)
		},
	}

	for name, derive := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			other, ok := derive()
			if !ok {
				t.Fatal("no key")
			}

			if other == base {
				t.Error("derives the same key as the original")
			}
		})
	}

	// And the same inputs derive the same key, or nothing would ever hit.
	again, _ := cache.Key(fp, org, conn, "SELECT 1", 100)
	if again != base {
		t.Error("the same inputs derived two different keys")
	}
}

/*
The key's parts cannot be rearranged into each other.

Every element is length-prefixed for this reason. Without it a connection id
ending in one string and a statement beginning with another can produce the
same bytes as a different pair, and two different questions that hash alike is
the one failure this whole design exists to prevent.
*/
func TestKeyPartsCannotBeConfusedWithEachOther(t *testing.T) {
	t.Parallel()

	var (
		cache = NewCache()
		org   = uuid.New()
		conn  = uuid.New()
		fp    = fingerprintFor(t, authz.RelationAnalyst)
	)

	first, _ := cache.Key(fp, org, conn, "ab", 1)
	second, _ := cache.Key(fp, org, conn, "a", 1)
	third, _ := cache.Key(fp, org, conn, "b", 1)

	if first == second || first == third || second == third {
		t.Error("statements of different lengths collided")
	}
}

// A bump makes every key derived from that connection unreachable, and leaves
// other connections alone.
func TestABumpedGenerationChangesOnlyItsOwnConnection(t *testing.T) {
	t.Parallel()

	var (
		cache = NewCache()
		org   = uuid.New()
		mine  = uuid.New()
		yours = uuid.New()
		fp    = fingerprintFor(t, authz.RelationAnalyst)
	)

	mineBefore, _ := cache.Key(fp, org, mine, "SELECT 1", 100)
	yoursBefore, _ := cache.Key(fp, org, yours, "SELECT 1", 100)

	cache.Invalidate(mine)

	mineAfter, _ := cache.Key(fp, org, mine, "SELECT 1", 100)
	yoursAfter, _ := cache.Key(fp, org, yours, "SELECT 1", 100)

	if mineAfter == mineBefore {
		t.Error("an invalidated connection derives the same key as before")
	}

	if yoursAfter != yoursBefore {
		t.Error("invalidating one connection changed another's key")
	}
}

// The cache evicts least-recently-used until it is back inside its budget.
func TestTheCacheEvictsToStayWithinItsBudget(t *testing.T) {
	t.Parallel()

	cache := NewCache(WithMaxBytes(100), WithMaxEntryBytes(60))

	put := func(key string, size int64) bool {
		return cache.Put(t.Context(), key, testColumns(), [][]any{{key}}, false, size)
	}

	if !put("a", 50) || !put("b", 50) {
		t.Fatal("two entries inside the budget were refused")
	}

	// Touch a, so b is the least recently used.
	if _, ok := cache.Get(t.Context(), "a"); !ok {
		t.Fatal("a is not in the cache")
	}

	if !put("c", 50) {
		t.Fatal("a third entry was refused")
	}

	if _, ok := cache.Get(t.Context(), "b"); ok {
		t.Error("the least recently used entry survived eviction")
	}

	if _, ok := cache.Get(t.Context(), "a"); !ok {
		t.Error("the recently used entry was evicted instead")
	}

	if stats := cache.Stats(); stats.Bytes > 100 {
		t.Errorf("the cache holds %d bytes, over its 100 byte budget", stats.Bytes)
	}
}

// A single result larger than the per-entry budget is refused outright.
func TestAnOversizedResultIsRefusedRatherThanTrimmed(t *testing.T) {
	t.Parallel()

	cache := NewCache(WithMaxBytes(1000), WithMaxEntryBytes(100))

	if cache.Put(t.Context(), "big", testColumns(), [][]any{{"x"}}, false, 101) {
		t.Error("an entry over the per-entry budget was accepted")
	}

	if stats := cache.Stats(); stats.Entries != 0 {
		t.Errorf("the cache holds %d entries after refusing one", stats.Entries)
	}
}

// An entry past its TTL is not served, and is dropped when it is found.
func TestAnEntryPastItsTTLIsDropped(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	cache := NewCache(WithTTL(10*time.Second), withCacheClock(func() time.Time { return now }))

	cache.Put(t.Context(), "k", testColumns(), [][]any{{"x"}}, false, 10)

	now = now.Add(11 * time.Second)

	if _, ok := cache.Get(t.Context(), "k"); ok {
		t.Fatal("an expired entry was served")
	}

	if stats := cache.Stats(); stats.Entries != 0 {
		t.Errorf("the expired entry is still held: %d entries", stats.Entries)
	}
}

/*
Two readers of one entry cannot disturb each other.

A replay hands the caller a row, and [connectors.Stream] says that row is valid
only until the next Next -- so a caller that writes to it is already
misbehaving. What this checks is the blast radius: on a driver-backed stream
that caller corrupts only itself, and a cached entry is shared, so without a
per-reader copy it would corrupt everybody.
*/
func TestOneReaderCannotCorruptAnother(t *testing.T) {
	t.Parallel()

	cache := NewCache()
	cache.Put(t.Context(), "k", testColumns(), [][]any{{"original"}}, false, 10)

	first, ok := cache.Get(t.Context(), "k")
	if !ok {
		t.Fatal("the entry is missing")
	}

	first.Next()
	first.Row()[0] = "vandalized"

	second, ok := cache.Get(t.Context(), "k")
	if !ok {
		t.Fatal("the entry is missing on the second read")
	}

	second.Next()

	if got := second.Row()[0]; got != "original" {
		t.Errorf("the second reader saw %q; one reader's write reached another", got)
	}
}

// A replay reports the columns and the truncation flag it was stored with.
func TestAReplayCarriesTheWholeResult(t *testing.T) {
	t.Parallel()

	cache := NewCache()
	cache.Put(t.Context(), "k", testColumns(), [][]any{{"a"}, {"b"}}, true, 10)

	stream, ok := cache.Get(t.Context(), "k")
	if !ok {
		t.Fatal("the entry is missing")
	}

	if cols := stream.Columns(); len(cols) != 1 || cols[0].Name != "id" {
		t.Errorf("columns = %+v", cols)
	}

	var seen []any

	for stream.Next() {
		seen = append(seen, stream.Row()[0])
	}

	if len(seen) != 2 || seen[0] != "a" || seen[1] != "b" {
		t.Errorf("replayed %v, want [a b]", seen)
	}

	if !stream.Truncated() {
		t.Error("a truncated result replayed as complete")
	}

	if err := stream.Err(); err != nil {
		t.Errorf("Err = %v on a cached result", err)
	}

	// Closing twice is part of the Stream contract.
	if err := stream.Close(); err != nil {
		t.Errorf("close: %v", err)
	}

	if err := stream.Close(); err != nil {
		t.Errorf("second close: %v", err)
	}
}

// --- helpers -----------------------------------------------------------------

func testColumns() []connectors.Column {
	return []connectors.Column{{
		Name: "id", SourceType: "TEXT",
		Type: datatype.Type{Kind: datatype.String, Source: "TEXT"}, Position: 1,
	}}
}

// fingerprintOrg is a fixed tenant, so two fingerprints built here differ only
// where the test meant them to.
var fingerprintOrg = uuid.MustParse("11111111-1111-1111-1111-111111111111")

/*
fingerprintFor resolves a fingerprint for a caller holding one role.

Through [policy.Resolve] rather than constructed directly, so these tests
exercise the same derivation the pipeline does -- a helper that built the hash
itself would keep passing after the real one changed.
*/
func fingerprintFor(t *testing.T, role authz.Relation) policy.Fingerprint {
	t.Helper()

	fp, err := policy.Resolve(t.Context(), fixedGranter{role},
		tenant.MustNewScope(fingerprintOrg, uuid.NullUUID{UUID: uuid.New(), Valid: true}))
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}

	return fp
}

// fixedGranter reports one standing, whoever asks.
type fixedGranter struct{ role authz.Relation }

func (g fixedGranter) Grants(
	context.Context, authz.Subject, authz.Object,
) ([]authz.Relation, error) {
	return []authz.Relation{g.role}, nil
}
