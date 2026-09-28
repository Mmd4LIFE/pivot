package query

import (
	"context"
	"net/url"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

/*
Cancellation reaches the source, through the pipeline.

The connector package already proves its own cancellation works when a test
calls it directly. That is not the same claim. Between a caller and the source
there is now a pipeline that wraps the stream, holds a connector and writes a
log row, and any one of those could hold the context, swallow the cancellation
or keep the connection -- leaving a query burning a warehouse's CPU while
Pivot reports it as stopped.

So this test observes the source. pg_stat_activity is where PostgreSQL says
what it is actually running, and a marker comment in the statement is what
makes one query findable among everything else on a shared test database.
*/

const cancelMarker = "pivot_pipeline_cancel_probe"

func TestCancellationReachesTheSourceThroughThePipeline(t *testing.T) {
	t.Parallel()

	f, observer := newPostgresFixture(t)

	statement := "SELECT pg_sleep(30) /* " + cancelMarker + " */"

	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()

	var (
		wg      sync.WaitGroup
		ex      *Execution
		execErr error
	)

	wg.Add(1)

	go func() {
		defer wg.Done()

		ex, execErr = f.executor.Execute(ctx, Request{ConnectionID: f.connID, SQL: statement})
		if execErr != nil {
			return
		}

		for ex.Stream.Next() {
		}

		_ = ex.Stream.Close()
	}()

	// It has to be running on the source before canceling it proves anything.
	if !waitForSource(t, observer, true) {
		t.Fatal("the query never reached PostgreSQL; there was nothing to cancel")
	}

	cancel()

	// Checked before waiting for the goroutine, and that order is the test.
	// pg_sleep(30) ends on its own eventually, so a pipeline that merely
	// stopped listening would still show an idle source once wg.Wait
	// returned -- thirty seconds later, with the warehouse having done all
	// the work. Asking now is asking whether cancellation landed.
	if !waitForSource(t, observer, false) {
		t.Fatal("PostgreSQL is still running the query after the pipeline canceled it")
	}

	wg.Wait()

	if execErr != nil {
		// Canceled during the connector's Stream call rather than during the
		// read. Either is a correct outcome for a query canceled this early;
		// what is not correct is the source still running it, and that has
		// just been checked.
		return
	}

	entry, err := f.repos.QueryLog.Get(f.ctx, ex.LogID)
	if err != nil {
		t.Fatalf("get the log entry: %v", err)
	}

	// The completing write survives the cancellation that caused it. Writing
	// the outcome through the context that was just canceled would lose
	// exactly the entries an operator most wants.
	if entry.State != repo.StateCanceled {
		t.Errorf("State = %q, want %q", entry.State, repo.StateCanceled)
	}

	if !entry.FinishedAt.Valid {
		t.Error("a canceled query was left with no finish time")
	}
}

/*
waitForSource polls pg_stat_activity until the probe query is present or
absent as asked, and reports whether it got there.

Polling rather than sleeping: how long a cancellation takes to land is the
source's business, and a fixed sleep would be either flaky or slow.
*/
func waitForSource(t *testing.T, observer *store.DB, want bool) bool {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		var running int

		err := observer.QueryRowContext(context.Background(), `
			SELECT count(*) FROM pg_stat_activity
			WHERE query LIKE '%`+cancelMarker+`%'
			  AND query NOT LIKE '%pg_stat_activity%'
			  AND state = 'active'`).Scan(&running)
		if err != nil {
			t.Fatalf("read pg_stat_activity: %v", err)
		}

		if (running > 0) == want {
			return true
		}

		time.Sleep(50 * time.Millisecond)
	}

	return false
}

// newPostgresFixture builds a pipeline whose source is the development
// PostgreSQL, plus an independent handle for watching what that PostgreSQL is
// doing.
func newPostgresFixture(t *testing.T) (*fixture, *store.DB) {
	t.Helper()

	raw := os.Getenv(PostgresURLEnv)
	if raw == "" {
		t.Skipf("%s not set; run `make test-all` to include PostgreSQL", PostgresURLEnv)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %s: %v", PostgresURLEnv, err)
	}

	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("port in %s: %v", PostgresURLEnv, err)
	}

	password, _ := parsed.User.Password()

	db := openPivotDB(t)
	repos := repo.New(db, repo.WithSecrets(testCipher(t)))

	org, err := repos.System().CreateOrganization(t.Context(),
		repo.CreateOrganization{Name: "Acme", Slug: "acme"})
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	userID := uuid.New()
	ctx := tenant.WithScope(t.Context(),
		tenant.MustNewScope(org.ID, uuid.NullUUID{UUID: userID, Valid: true}))

	conn, err := repos.Connections.Create(ctx, repo.CreateConnection{
		Slug: "warehouse", Name: "The warehouse", Kind: string(connectors.KindPostgres),
		Host: parsed.Hostname(), Port: int64(port),
		Database: parsed.Path[1:], Username: parsed.User.Username(),
		Password: password, SSLMode: parsed.Query().Get("sslmode"), IsEnabled: true,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	observer, err := store.Open(t.Context(), observerConfig(raw), discardLogger())
	if err != nil {
		t.Fatalf("open the observer: %v", err)
	}

	t.Cleanup(func() { _ = observer.Close() })

	checker := &fakeChecker{allow: true}

	return &fixture{
		executor: NewExecutor(repos, checker),
		repos:    repos,
		ctx:      ctx,
		orgID:    org.ID,
		userID:   userID,
		connID:   conn.ID,
		checker:  checker,
	}, observer
}
