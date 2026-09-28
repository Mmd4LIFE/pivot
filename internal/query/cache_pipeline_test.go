package query

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/policy"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

/*
The cache, through the pipeline.

Every test here goes through [Executor.Execute] with the real authorization
graph behind it, and proves what it claims by counting how many times a
connector was opened. Counting opens is the only assertion that distinguishes
"the cache returned the right rows" from "the cache returned the right rows
because it went and fetched them again".
*/

/*
Two callers under different policy sets cannot share a cached entry.

This is P1-QE-009 and the reason the cache key has a fingerprint in it rather
than a check beside it. An analyst and an administrator resolve to different
permission sets, so they derive different keys -- they are not refused each
other's entry, they are structurally unable to name it.

Proven by opens: if the second caller could reach the first one's entry, the
source would be opened once for two queries. It is opened twice.
*/
func TestTwoPolicySetsCannotShareACachedResult(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)

	analystCtx := f.as(t, f.analyst)
	adminCtx := f.as(t, f.admin)

	first, err := f.executor.Execute(analystCtx, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("analyst: %v", err)
	}

	drain(t, first)

	// The same query, the same connection, the same tenant -- and a caller
	// whose standing differs.
	second, err := f.executor.Execute(adminCtx, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("admin: %v", err)
	}

	drain(t, second)

	if second.CacheStatus == CacheHit {
		t.Fatal("an administrator was served a result cached for an analyst")
	}

	if opens := f.opens.Load(); opens != 2 {
		t.Errorf("the source was opened %d times for two different policy sets, want 2", opens)
	}

	// And the keys really are different, checked directly so a failure says
	// which half is wrong rather than only that the counts disagreed.
	analystKey, ok := f.key(analystCtx, t)
	if !ok {
		t.Fatal("the analyst has no cache key")
	}

	adminKey, ok := f.key(adminCtx, t)
	if !ok {
		t.Fatal("the administrator has no cache key")
	}

	if analystKey == adminKey {
		t.Error("two different policy sets derived the same cache key")
	}
}

// The same caller asking twice pays once, and the log says which was which.
func TestTheSameQuestionAskedTwiceCostsOnce(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)
	ctx := f.as(t, f.analyst)

	first, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("first: %v", err)
	}

	if rows := drain(t, first); rows != 2 {
		t.Fatalf("first read %d rows, want 2", rows)
	}

	second, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if rows := drain(t, second); rows != 2 {
		t.Fatalf("the cached read produced %d rows, want 2", rows)
	}

	if opens := f.opens.Load(); opens != 1 {
		t.Errorf("the source was opened %d times for two identical queries, want 1", opens)
	}

	if second.CacheStatus != CacheHit {
		t.Errorf("the second execution reports %q, want hit", second.CacheStatus)
	}

	// And the query log says so -- both executions are on record, which is
	// the half a cache is most likely to quietly break.
	entries, err := f.repos.QueryLog.List(ctx, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("two executions left %d log entries", len(entries))
	}

	// Newest first.
	if entries[0].CacheStatus != CacheHit {
		t.Errorf("the cached execution logged cache_status %q, want hit", entries[0].CacheStatus)
	}

	if entries[1].CacheStatus != CacheMiss {
		t.Errorf("the first execution logged cache_status %q, want miss", entries[1].CacheStatus)
	}
}

// A cached result is the same answer, not merely a fast one.
func TestACachedResultIsIdenticalToALiveOne(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)
	ctx := f.as(t, f.analyst)

	live := collectRows(ctx, t, f, emeaQuery)
	cached := collectRows(ctx, t, f, emeaQuery)

	if f.opens.Load() != 1 {
		t.Fatalf("the second read was not served from cache: %d opens", f.opens.Load())
	}

	if len(live.columns) != len(cached.columns) {
		t.Fatalf("columns differ: %d live, %d cached", len(live.columns), len(cached.columns))
	}

	for i := range live.columns {
		if live.columns[i] != cached.columns[i] {
			t.Errorf("column %d differs:\n live   %+v\n cached %+v",
				i, live.columns[i], cached.columns[i])
		}
	}

	if len(live.rows) != len(cached.rows) {
		t.Fatalf("row counts differ: %d live, %d cached", len(live.rows), len(cached.rows))
	}

	for i := range live.rows {
		for j := range live.rows[i] {
			if live.rows[i][j] != cached.rows[i][j] {
				t.Errorf("row %d column %d: live %v, cached %v",
					i, j, live.rows[i][j], cached.rows[i][j])
			}
		}
	}
}

/*
A truncated result stays truncated through the cache.

The row cap is part of the key, so the entry cached under one cap cannot be
served to a request asking for another. This checks the other half: that the
flag itself survives, because a signaled partial answer replayed without its
signal is a silent partial answer -- the failure Part 16 refused to ship and
the one a cache is the easiest place to reintroduce.
*/
func TestTruncationSurvivesTheCache(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)
	ctx := f.as(t, f.analyst)

	run := func() *Execution {
		ex, err := f.executor.Execute(ctx, Request{
			ConnectionID: f.connID, SQL: "SELECT id FROM orders", MaxRows: 1,
		})
		if err != nil {
			t.Fatalf("execute: %v", err)
		}

		return ex
	}

	first := run()
	drain(t, first)

	second := run()

	rows := drain(t, second)

	if second.CacheStatus != CacheHit {
		t.Fatalf("the second execution was not a hit: %q", second.CacheStatus)
	}

	if rows != 1 {
		t.Errorf("the cached result produced %d rows, want 1", rows)
	}

	entry, err := f.repos.QueryLog.Get(ctx, second.LogID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if !bool(entry.Truncated) {
		t.Error("a cached truncated result was logged as complete")
	}
}

// A different row cap is a different question, and must not be answered from
// the entry cached for the first one.
func TestADifferentRowCapIsADifferentEntry(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)
	ctx := f.as(t, f.analyst)

	one, err := f.executor.Execute(ctx, Request{
		ConnectionID: f.connID, SQL: "SELECT id FROM orders", MaxRows: 1,
	})
	if err != nil {
		t.Fatalf("first: %v", err)
	}

	drain(t, one)

	two, err := f.executor.Execute(ctx, Request{
		ConnectionID: f.connID, SQL: "SELECT id FROM orders", MaxRows: 3,
	})
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if rows := drain(t, two); rows != 3 {
		t.Errorf("the higher cap produced %d rows, want 3", rows)
	}

	if two.CacheStatus == CacheHit {
		t.Error("a request for three rows was served the entry cached for one")
	}
}

// Changing the connection makes everything cached from it unreachable.
func TestChangingAConnectionInvalidatesItsResults(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)
	ctx := f.as(t, f.analyst)

	first, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("first: %v", err)
	}

	drain(t, first)

	f.cache.Invalidate(f.connID)

	second, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	drain(t, second)

	if second.CacheStatus == CacheHit {
		t.Error("a result cached before the connection changed was still served")
	}

	if opens := f.opens.Load(); opens != 2 {
		t.Errorf("the source was opened %d times across an invalidation, want 2", opens)
	}
}

// An entry that outlives its TTL is not served.
func TestAnExpiredEntryIsNotServed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	f := newCacheFixture(t, withCacheClock(clock), WithTTL(30*time.Second))
	ctx := f.as(t, f.analyst)

	first, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("first: %v", err)
	}

	drain(t, first)

	now = now.Add(31 * time.Second)

	second, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	drain(t, second)

	if second.CacheStatus == CacheHit {
		t.Error("an entry past its TTL was served")
	}
}

/*
A stream the caller walked away from is not cached.

The buffer at that point holds however many rows the caller happened to read,
and storing it would serve a partial answer as a whole one -- with nothing on
it to say so, unlike a truncated result, which at least carries a flag.
*/
func TestAnAbandonedStreamIsNotCached(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)
	ctx := f.as(t, f.analyst)

	ex, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: "SELECT id FROM orders"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// One row, then leave -- which is what a closed browser tab looks like.
	ex.Stream.Next()

	if cerr := ex.Stream.Close(); cerr != nil {
		t.Fatalf("close: %v", cerr)
	}

	again, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: "SELECT id FROM orders"})
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if rows := drain(t, again); rows != 3 {
		t.Errorf("read %d rows, want the full 3", rows)
	}

	if again.CacheStatus == CacheHit {
		t.Fatal("a partially read result was cached and served as complete")
	}

	entry, err := f.repos.QueryLog.Get(ctx, ex.LogID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if entry.CacheStatus != CacheUncached {
		t.Errorf("the abandoned execution logged %q, want uncached", entry.CacheStatus)
	}
}

/*
A result too large for the budget is not cached, and reading it costs no more
memory than it did before the cache existed.

This is the property Part 20-a measured and this part could have quietly
undone. The tee accumulates while the result is small and abandons the moment
it is not, so peak memory is bounded by the budget rather than by the answer.
*/
func TestALargeResultIsStreamedRatherThanCached(t *testing.T) {
	t.Parallel()

	// A budget small enough that the fixture's three rows cross it.
	f := newCacheFixture(t, WithMaxEntryBytes(8))
	ctx := f.as(t, f.analyst)

	ex, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: "SELECT id, region FROM orders"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if rows := drain(t, ex); rows != 3 {
		t.Fatalf("read %d rows, want 3; the tee interfered with the stream", rows)
	}

	entry, err := f.repos.QueryLog.Get(ctx, ex.LogID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if entry.CacheStatus != CacheUncached {
		t.Errorf("an over-budget result logged %q, want uncached", entry.CacheStatus)
	}

	if stats := f.cache.Stats(); stats.Entries != 0 {
		t.Errorf("the cache holds %d entries after an over-budget result", stats.Entries)
	}

	// And asking again really does go back to the source.
	again, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: "SELECT id, region FROM orders"})
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	drain(t, again)

	if opens := f.opens.Load(); opens != 2 {
		t.Errorf("the source was opened %d times, want 2", opens)
	}
}

// A query that fails leaves nothing behind to be served later.
func TestAFailedQueryIsNotCached(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)
	ctx := f.as(t, f.analyst)

	if _, err := f.executor.Execute(ctx, Request{
		ConnectionID: f.connID, SQL: "SELECT nope FROM orders",
	}); err == nil {
		t.Fatal("a query against a missing column succeeded")
	}

	if stats := f.cache.Stats(); stats.Entries != 0 {
		t.Errorf("a failed query left %d cache entries", stats.Entries)
	}
}

/*
A caller whose policy set cannot be resolved is not cached and is not served
from cache.

The fail-closed direction, and the one that matters: treating an unresolvable
caller as holding the empty policy set makes every such caller collide with
every other, which is the breach the fingerprint exists to prevent. The cost of
getting it right is a cache miss.
*/
func TestAnUnresolvableCallerIsNeverCached(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)
	ctx := f.as(t, f.analyst)

	warm, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("warm: %v", err)
	}

	drain(t, warm)

	// The same executor with a resolver that cannot answer.
	broken := NewExecutor(f.repos, f.checker,
		withOpener(f.countingOpener()), WithCache(f.cache, brokenGranter{}))

	ex, err := broken.Execute(ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	drain(t, ex)

	if ex.CacheStatus == CacheHit {
		t.Fatal("a caller who could not be fingerprinted was served from the cache")
	}

	if ex.CacheStatus != CacheUncached {
		t.Errorf("CacheStatus = %q, want uncached", ex.CacheStatus)
	}

	if opens := f.opens.Load(); opens != 2 {
		t.Errorf("the source was opened %d times, want 2", opens)
	}
}

// And the fingerprint itself refuses, rather than returning something usable.
func TestAnUnresolvableFingerprintIsAnError(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)

	fp, err := policy.Resolve(f.as(t, f.analyst), brokenGranter{},
		mustScope(t, f.orgID, f.analyst))
	if !errors.Is(err, policy.ErrUnresolved) {
		t.Fatalf("err = %v, want ErrUnresolved", err)
	}

	if fp.Resolved() {
		t.Error("an unresolvable caller produced a usable fingerprint")
	}

	if _, ok := f.cache.Key(fp, f.orgID, f.connID, emeaQuery, 100); ok {
		t.Error("an unresolved fingerprint produced a cache key")
	}
}

/*
Background work caches under its own fingerprint.

A scheduled refresh has no person behind it, so there is nothing to resolve
grants for. Giving it its own fingerprint rather than the empty one keeps it
from sharing an entry with a caller who happens to hold no grants -- which is
what an unauthenticated request looks like from here.
*/
func TestSystemWorkHasItsOwnFingerprint(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)
	ctx := f.as(t, f.analyst)

	person, err := policy.Resolve(ctx, f.granter(t), mustScope(t, f.orgID, f.analyst))
	if err != nil {
		t.Fatalf("resolve the person: %v", err)
	}

	system, err := policy.Resolve(ctx, f.granter(t), tenant.MustNewScope(f.orgID, uuid.NullUUID{}))
	if err != nil {
		t.Fatalf("resolve the system scope: %v", err)
	}

	if !system.Resolved() {
		t.Fatal("background work has no fingerprint at all")
	}

	if person.Equal(system) {
		t.Error("background work and a user share a fingerprint")
	}
}

// --- helpers -----------------------------------------------------------------

type collected struct {
	columns []string
	rows    [][]any
}

func collectRows(ctx context.Context, t *testing.T, f *cacheFixture, sql string) collected {
	t.Helper()

	ex, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: sql})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	out := collected{}
	for _, c := range ex.Stream.Columns() {
		out.columns = append(out.columns, c.Name+":"+c.SourceType)
	}

	for ex.Stream.Next() {
		row := ex.Stream.Row()
		copied := make([]any, len(row))
		copy(copied, row)
		out.rows = append(out.rows, copied)
	}

	if serr := ex.Stream.Err(); serr != nil {
		t.Fatalf("stream: %v", serr)
	}

	if cerr := ex.Stream.Close(); cerr != nil {
		t.Fatalf("close: %v", cerr)
	}

	return out
}

// key derives the cache key this caller would use, for a test that wants to
// compare two of them directly.
func (f *cacheFixture) key(ctx context.Context, t *testing.T) (string, bool) {
	t.Helper()

	plan, err := f.executor.plan(ctx, emeaQuery, Request{ConnectionID: f.connID})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	scope, err := tenant.FromContext(ctx)
	if err != nil {
		t.Fatalf("scope: %v", err)
	}

	return f.executor.cacheKey(ctx, scope, plan)
}

// granter is the fixture's checker as a [authz.Granter].
func (f *cacheFixture) granter(t *testing.T) authz.Granter {
	t.Helper()

	g, ok := f.checker.(authz.Granter)
	if !ok {
		t.Fatal("the production checker does not resolve grants")
	}

	return g
}

// mustScope builds a scope acting as one of the seeded users.
func mustScope(t *testing.T, orgID, userID uuid.UUID) tenant.Scope {
	t.Helper()

	return tenant.MustNewScope(orgID, uuid.NullUUID{UUID: userID, Valid: true})
}

/*
brokenGranter cannot resolve anybody.

It stands for every way the authorization store can be unavailable at the
moment a query arrives -- the database is down, the context expired, a
migration is mid-flight. What matters is that none of them may produce a usable
fingerprint.
*/
type brokenGranter struct{}

func (brokenGranter) Grants(
	context.Context, authz.Subject, authz.Object,
) ([]authz.Relation, error) {
	return nil, errors.New("the authorization store is unavailable")
}

/*
Editing a connection through the repository invalidates its cached results.

Through the event bus rather than by calling Invalidate, because the bus is the
part that has to work: a cache invalidated by hand at each write site is
correct until somebody adds the write site that forgets, and this is the test
that would notice.
*/
func TestEditingAConnectionThroughTheRepositoryInvalidatesTheCache(t *testing.T) {
	t.Parallel()

	f := newCacheFixture(t)
	f.cache.Watch(f.repos.Events())

	ctx := f.as(t, f.analyst)

	first, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("first: %v", err)
	}

	drain(t, first)

	// A full round trip, which is how the repository's optimistic locking
	// wants an update done: read it, change one thing, write it back.
	current, err := f.repos.Connections.Get(ctx, f.connID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if _, uerr := f.repos.Connections.Update(ctx, repo.UpdateConnection{
		ID: current.ID, Slug: current.Slug, Name: current.Name,
		Description: "a new description",
		Host:        current.Host, Port: current.Port, Database: current.Database,
		Username: current.Username, Password: current.Password,
		SSLMode: current.SslMode, Options: current.Options,
		MaxOpenConns: current.MaxOpenConns, MaxRows: current.MaxRows,
		QueryTimeoutSeconds: current.QueryTimeoutSeconds,
		IsEnabled:           bool(current.IsEnabled), Version: current.Version,
	}); uerr != nil {
		t.Fatalf("update: %v", uerr)
	}

	second, err := f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: emeaQuery})
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	drain(t, second)

	if second.CacheStatus == CacheHit {
		t.Error("a result cached before the connection was edited was still served")
	}
}
