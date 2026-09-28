package catalog

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

/*
The catalog sync, as scheduled work.

Two jobs rather than one, because "sync everything" and "sync this connection"
fail differently and should not share a retry.

[SweepArgs] runs on a timer, lists the connections and enqueues one
[SyncArgs] each. It touches no warehouse, so it is fast and its failure means
Pivot's own database is unwell.

[SyncArgs] reaches one source. It is slow, it fails for reasons outside
Pivot -- a warehouse down, a credential rotated -- and retrying it must not
drag every other connection's sync along with it. One connection's outage stays
one connection's problem.

River elects a leader across every process sharing the metadata database, and
only the leader inserts periodic jobs. That is what keeps two Pivots from both
syncing the same connection, and it is the reason this is a periodic job rather
than a ticker.
*/

// DefaultInterval is how often the catalog is refreshed.
//
// Fifteen minutes rather than hourly: a column that changed type is a chart
// rendering nonsense until the next sync, and rather than daily because every
// sync reads a source somebody else pays for.
const DefaultInterval = 15 * time.Minute

// SweepArgs asks for every connection to be synced.
type SweepArgs struct{}

// Kind is River's name for this job. Stored in the database, so it is
// permanent: renaming it orphans every queued row.
func (SweepArgs) Kind() string { return "catalog.sweep" }

// SyncArgs asks for one connection to be synced.
type SyncArgs struct {
	OrgID        uuid.UUID `json:"org_id"`
	ConnectionID uuid.UUID `json:"connection_id"`
	Slug         string    `json:"slug"`
}

func (SyncArgs) Kind() string { return "catalog.sync" }

/*
InsertOpts makes a connection's sync unique while it is pending or running.

Without it, a source slower than the interval accumulates a queue of syncs that
each do the same work against the same warehouse -- and the backlog grows for
exactly the connections least able to take the load.
*/
func (SyncArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		UniqueOpts: river.UniqueOpts{
			ByArgs: true,
			ByState: []rivertype.JobState{
				rivertype.JobStateAvailable,
				rivertype.JobStateRunning,
				rivertype.JobStateScheduled,
				rivertype.JobStateRetryable,
				rivertype.JobStatePending,
			},
		},
	}
}

// SweepWorker enqueues one sync per connection.
//
// It takes the repositories concretely rather than through an interface. The
// worker is glue -- list, loop, enqueue -- and an interface with five methods
// to make glue mockable is more code than the glue. What is worth testing in
// isolation is the reconciliation, and that already is.
type SweepWorker struct {
	river.WorkerDefaults[SweepArgs]

	Repos *repo.Repositories
	Log   *slog.Logger
}

// Work lists every connection in every organization and enqueues a sync.
func (w *SweepWorker) Work(ctx context.Context, _ *river.Job[SweepArgs]) error {
	// Every organization, in pages, because an instance with a thousand
	// tenants should not build one slice of all of them to loop over.
	orgs, err := w.Repos.System().ListOrganizations(ctx, allOrganizations, 0)
	if err != nil {
		return fmt.Errorf("list organizations: %w", err)
	}

	client := river.ClientFromContext[*sql.Tx](ctx)

	enqueued := 0

	for _, org := range orgs {
		scoped, serr := systemScope(ctx, org.ID)
		if serr != nil {
			return fmt.Errorf("scope to %s: %w", org.Slug, serr)
		}

		conns, cerr := w.Repos.Connections.List(scoped)
		if cerr != nil {
			return fmt.Errorf("list connections for %s: %w", org.Slug, cerr)
		}

		for _, conn := range conns {
			if !bool(conn.IsEnabled) {
				continue
			}

			if _, ierr := client.Insert(ctx, SyncArgs{
				OrgID: org.ID, ConnectionID: conn.ID, Slug: conn.Slug,
			}, nil); ierr != nil {
				return fmt.Errorf("enqueue a sync for %s: %w", conn.Slug, ierr)
			}

			enqueued++
		}
	}

	w.Log.Info("catalog sweep enqueued syncs", "connections", enqueued)

	return nil
}

// SyncWorker reconciles one connection's catalog.
type SyncWorker struct {
	river.WorkerDefaults[SyncArgs]

	Repos *repo.Repositories
	Log   *slog.Logger
}

// Timeout gives a sync room for a large schema without letting a hung source
// hold a worker forever.
func (w *SyncWorker) Timeout(*river.Job[SyncArgs]) time.Duration { return 10 * time.Minute }

// Work syncs one connection and logs what changed.
func (w *SyncWorker) Work(ctx context.Context, job *river.Job[SyncArgs]) error {
	scoped, err := systemScope(ctx, job.Args.OrgID)
	if err != nil {
		return fmt.Errorf("scope to the organization: %w", err)
	}

	conn, err := w.Repos.Connections.Get(scoped, job.Args.ConnectionID)
	if err != nil {
		// A connection deleted between the sweep and this job is not a
		// failure. Retrying would never succeed, and River would keep trying
		// until it gave up and filed it as an error somebody has to read.
		w.Log.Info("the connection is gone; nothing to sync", "slug", job.Args.Slug)

		return nil
	}

	connector, err := connectors.Open(configFor(conn))
	if err != nil {
		return fmt.Errorf("open %s: %w", conn.Slug, err)
	}

	defer func() { _ = connector.Close() }()

	report, err := NewSyncer(w.Repos.Catalog).Sync(scoped, conn.ID, connector)
	if err != nil {
		return fmt.Errorf("sync %s: %w", conn.Slug, err)
	}

	// Logged at info whether or not anything changed, because "the sync ran
	// and found nothing" is the observation that distinguishes a quiet
	// schema from a scheduler that has stopped.
	w.Log.Info("catalog synced", "slug", conn.Slug, "summary", report.Summary())

	for _, change := range report.Changes {
		w.Log.Info("catalog change", "slug", conn.Slug, "change", change.String())
	}

	return nil
}

// configFor turns a stored connection into a connector configuration.
func configFor(c model.Connection) connectors.Config {
	return connectors.Config{
		Kind:                connectors.Kind(c.Kind),
		Host:                c.Host,
		Port:                int(c.Port),
		Database:            c.Database,
		Username:            c.Username,
		Password:            c.Password,
		SSLMode:             c.SslMode,
		MaxOpenConns:        int(c.MaxOpenConns),
		QueryTimeoutSeconds: int(c.QueryTimeoutSeconds),
		MaxRows:             c.MaxRows,
	}
}

// allOrganizations is the page size the sweep reads with. Large enough that
// one page covers every realistic instance and small enough to be a page
// rather than a promise there will never be more.
const allOrganizations = 10_000

// systemScope builds a context acting as Pivot itself within one tenant.
//
// Background work has no user, and the alternative to a system scope is an
// unscoped query -- which the isolation harness exists to make impossible.
func systemScope(ctx context.Context, orgID uuid.UUID) (context.Context, error) {
	scope, err := tenant.NewSystemScope(orgID)
	if err != nil {
		return nil, err
	}

	return tenant.WithScope(ctx, scope), nil
}
