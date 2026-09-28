package connectors_test

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
)

/*
Per-connection resource governance, on every connector rather than one.

Three limits travel with a connection: how many rows a result may carry, how
long a query may run, and how many connections it may hold. The first two are
conformance properties, checked against every connector by its own suite. The
third is here, because proving a pool caps requires looking at the pool rather
than at the answer.

Driven off a table of every connector this run can reach, so a fourth one gets
covered by existing here rather than by somebody remembering.
*/

// governable is a connector this test can reach, and the SQL it needs.
type governable struct {
	config connectors.Config
	series func(n int) string
}

func governables(t *testing.T) map[string]governable {
	t.Helper()

	out := map[string]governable{
		// Always available: a file in a temporary directory, no container and
		// no environment variable.
		"sqlite": {
			config: connectors.Config{
				Kind: connectors.KindSQLite, Database: seedSQLite(t),
			},
			series: sqliteSeries,
		},
	}

	if os.Getenv(postgresURLEnv) != "" {
		out["postgres"] = governable{
			config: liveTarget(t).config(),
			series: func(n int) string {
				return fmt.Sprintf("SELECT i FROM generate_series(1, %d) AS i", n)
			},
		}
	}

	if os.Getenv(mysqlURLEnv) != "" {
		cfg := mysqlConfig(t)
		// The suite's own recursion ceiling, since MySQL has no
		// generate_series and stops a recursive CTE at 1000 by default.
		cfg.Options = map[string]string{"cte_max_recursion_depth": "100000"}

		out["mysql"] = governable{
			config: cfg,
			series: func(n int) string {
				return fmt.Sprintf(
					"WITH RECURSIVE s (i) AS ("+
						"SELECT 1 UNION ALL SELECT i + 1 FROM s WHERE i < %d"+
						") SELECT i FROM s", n)
			},
		}
	}

	return out
}

/*
The pool cap binds, and binding it does not break anything.

Two assertions, and the second is the one that makes the first mean something.

That `MaxOpenConnections` reads back as 2 only says the setting was applied.
What matters is `WaitCount`: the number of times a goroutine had to queue for a
connection. Greater than zero is proof the cap actually limited concurrency
during this run, rather than the queries happening to finish one at a time
anyway -- which is how a pool test passes on a fast machine while the cap does
nothing.

And every query still has to succeed. A cap that deadlocked or errored under
contention would be worse than no cap at all.
*/
func TestEveryConnectorHonorsItsPoolCap(t *testing.T) {
	t.Parallel()

	for name, target := range governables(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := target.config
			cfg.MaxOpenConns = 2
			cfg.QueryTimeoutSeconds = 30

			connector := open(t, cfg)

			pooled, ok := connector.(*connectors.SQLConnector)
			if !ok {
				t.Fatalf("%s is a %T, which has no pool to inspect", name, connector)
			}

			// Enough rows that a query takes long enough to overlap with the
			// others, and few enough that sixteen of them are quick.
			const (
				concurrent = 16
				rows       = 20_000
			)

			var wait sync.WaitGroup

			failures := make([]error, concurrent)

			for i := range failures {
				wait.Add(1)

				go func() {
					defer wait.Done()

					_, failures[i] = connector.Query(t.Context(), target.series(rows))
				}()
			}

			wait.Wait()

			for i, err := range failures {
				if err != nil {
					t.Fatalf("query %d of %d through a pool of 2 failed: %v", i, concurrent, err)
				}
			}

			stats := pooled.DB().Stats()

			if stats.MaxOpenConnections != 2 {
				t.Errorf("MaxOpenConnections = %d, want 2", stats.MaxOpenConnections)
			}

			if stats.OpenConnections > 2 {
				t.Errorf("the pool holds %d connections, which is over its cap of 2",
					stats.OpenConnections)
			}

			if stats.WaitCount == 0 {
				t.Errorf("%d concurrent queries never once waited for a connection, "+
					"so the cap of 2 did not bind and this proves nothing", concurrent)
			}

			t.Logf("%s: %d queries, %d waits, %d connections open at the end",
				name, concurrent, stats.WaitCount, stats.OpenConnections)
		})
	}
}

/*
A result over the cap is truncated with the flag, on every connector.

The conformance suite checks this per connector already. It is repeated here
against the *same* table of connectors as the pool cap, because these three
limits are one feature -- per-connection resource governance -- and a reader
asking "is it enforced everywhere" should find one answer rather than three
files.
*/
func TestEveryConnectorTruncatesWithASignal(t *testing.T) {
	t.Parallel()

	for name, target := range governables(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := target.config
			cfg.MaxRows = 50
			cfg.QueryTimeoutSeconds = 30

			result, err := open(t, cfg).Query(t.Context(), target.series(500))
			if err != nil {
				t.Fatalf("select 500 rows against a cap of 50: %v", err)
			}

			if len(result.Rows) != 50 {
				t.Errorf("got %d rows, want the cap of 50", len(result.Rows))
			}

			if !result.Truncated {
				t.Error("the result was cut at the cap and is not flagged truncated, " +
					"so everything above this treats a partial answer as the whole one")
			}
		})
	}
}
