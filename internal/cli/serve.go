package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"syscall"
	"time"

	"github.com/riverqueue/river"
	"github.com/spf13/cobra"

	"github.com/Mmd4LIFE/pivot/internal/api"
	"github.com/Mmd4LIFE/pivot/internal/auth"
	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/catalog"
	"github.com/Mmd4LIFE/pivot/internal/config"
	"github.com/Mmd4LIFE/pivot/internal/jobs"
	"github.com/Mmd4LIFE/pivot/internal/logging"
	"github.com/Mmd4LIFE/pivot/internal/observability"
	"github.com/Mmd4LIFE/pivot/internal/oidc"
	"github.com/Mmd4LIFE/pivot/internal/query"
	"github.com/Mmd4LIFE/pivot/internal/setup"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/version"
	"github.com/Mmd4LIFE/pivot/web"
)

// sweepRetention keeps revoked and expired rows around briefly rather than
// deleting them the moment they die. An investigation into "who was logged in
// when" needs them, and Phase 4's audit work will read them.
const sweepRetention = 7 * 24 * time.Hour

func newServeCmd(env Env, flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the Pivot server",
		Long: `Start the Pivot HTTP server.

The server responds to SIGTERM and SIGINT by failing its readiness probe,
draining in-flight requests, and then exiting. Load balancers see the
readiness change before the drain begins, so no request is dropped.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := loadConfig(cmd, env, flags)
			if err != nil {
				return err
			}
			cfg := res.Config

			// Logs go to stderr so stdout stays clean for command output —
			// which matters the moment anything pipes `pivot` into a tool.
			// Tracing first, so the logger below can be correlated and so the
			// startup lines themselves belong to a trace if one is running.
			shutdownTracing, terr := observability.Setup(cmd.Context(), observability.Config{
				Enabled:        cfg.Observability.Tracing.Enabled,
				Endpoint:       cfg.Observability.Tracing.Endpoint,
				Insecure:       cfg.Observability.Tracing.Insecure,
				SampleRatio:    cfg.Observability.Tracing.SampleRatio,
				ServiceName:    cfg.Observability.Tracing.ServiceName,
				ServiceVersion: version.Get().Version,
			}, logging.New(cfg.Log, env.Stderr))
			if terr != nil {
				return terr
			}

			defer func() {
				// A fresh context: cmd's is already canceled by the time this
				// runs, and a flush on a canceled context drops exactly the
				// spans describing whatever went wrong on the way out.
				flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				if ferr := shutdownTracing(flushCtx); ferr != nil {
					slog.Default().Warn("could not flush traces", logging.Err(ferr))
				}
			}()

			// WithTrace wraps the handler so every line carries its trace, and
			// it has to wrap the configured one rather than replace it -- the
			// operator's format and level are still theirs.
			log := logging.WithTrace(logging.New(cfg.Log, env.Stderr))
			slog.SetDefault(log)

			metrics, metricsHandler, shutdownMetrics, merr := observability.SetupMetrics(observability.MetricsConfig{
				Enabled:     cfg.Observability.Metrics.Enabled,
				ServiceName: cfg.Observability.Tracing.ServiceName,
			}, log)
			if merr != nil {
				return merr
			}

			defer func() {
				flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				if ferr := shutdownMetrics(flushCtx); ferr != nil {
					slog.Default().Warn("could not flush metrics", logging.Err(ferr))
				}
			}()

			info := version.Get()
			log.Info("starting pivot",
				slog.String("version", info.Version),
				slog.String("commit", info.Commit),
				slog.String("go", info.GoVersion),
				slog.String("config_file", sourceOrNone(res.SourceFile)),
			)

			db, err := store.Open(cmd.Context(), cfg.Database, log)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			if cfg.Database.AutoMigrate {
				if err := store.Migrate(cmd.Context(), db, log); err != nil {
					return err
				}

				// River's own tables, which are not in Pivot's migrations.
				// Auto-migrating one schema and not the other would leave the
				// job runner unable to start on a database Pivot had just
				// declared ready.
				if _, err := jobs.Migrate(cmd.Context(), db); err != nil {
					return err
				}
			} else if err := warnIfBehind(cmd, db, log); err != nil {
				return err
			}

			// NotifyContext cancels on the first signal and restores default
			// behavior on the second, so an impatient operator can still
			// force-quit a hung drain with a second Ctrl-C.
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			// The key for stored secrets, before anything can try to use one.
			// An instance whose key is missing says so here rather than
			// failing later, inside somebody's SSO login.
			resolved, serr := resolveSecrets(cfg, true, log)
			if serr != nil {
				return serr
			}

			log.Info("stored secrets are encrypted",
				slog.String("key_id", resolved.Keyring.PrimaryID()),
				slog.String("key_source", string(resolved.Source)))

			repos := repo.New(db, repo.WithSecrets(resolved.Keyring))
			authSvc := auth.NewService(repos, auth.PolicyFrom(cfg.Auth), log)

			// Expired sessions and stale login-attempt rows accumulate
			// otherwise — including one row per address a dictionary attack
			// ever tried. Phase 5's scheduler takes this over; until then,
			// once at startup is enough for a single-node install.
			if sessions, attempts, serr := authSvc.Sweep(ctx, sweepRetention); serr != nil {
				log.Warn("could not sweep expired sessions", logging.Err(serr))
			} else if sessions > 0 || attempts > 0 {
				log.Info("swept expired authentication records",
					slog.Int64("sessions", sessions),
					slog.Int64("login_attempts", attempts),
				)
			}

			if !cfg.Auth.CookieSecure {
				log.Warn("session cookie is not forced Secure",
					slog.String("hint",
						"set auth.cookieSecure when serving over TLS or behind a TLS proxy"),
				)
			}

			checker, cache := authz.New(repos)

			cookie := api.CookieConfig{
				Name:   cfg.Auth.CookieName,
				Path:   "/",
				Domain: cfg.Auth.CookieDomain,
				Secure: cfg.Auth.CookieSecure,
			}

			// One registry per process: it caches provider discovery, so
			// sharing it is what turns a per-login round trip into a per-hour
			// one.
			registry := oidc.NewRegistry()

			// The first run.
			//
			// A token is generated per process when none is configured, and
			// printed below. Without one, whoever reaches an unclaimed Pivot
			// first becomes its administrator -- which on a laptop is nobody
			// and on a network is whoever is scanning it.
			setupToken := cfg.Setup.Token
			if setupToken == "" {
				generated, terr := setup.NewToken()
				if terr != nil {
					return terr
				}

				setupToken = generated
			}

			setupSvc := setup.NewService(repos, setupToken)
			authHandler := api.NewAuthHandler(authSvc, repos, cookie, log)

			announceSetup(ctx, env, log, setupSvc, setupToken, cfg)

			srv := api.New(cfg.Server, log,
				api.WithCheck(api.Check{
					Name: "database",
					Func: db.HealthCheck,
				}),
				api.WithAuth(authHandler),
				api.WithSetup(api.NewSetupHandler(setupSvc, authHandler, log)),
				api.WithRoles(api.NewRoleHandler(repos, checker, cache, log)),
				api.WithOIDC(api.NewOIDCHandler(
					repos, registry, authSvc, cookie, cfg.Server.BaseURL, log)),
				api.WithSPA(web.Handler()),
				// Nil handler when metrics are off, which leaves /metrics
				// unregistered rather than serving an empty page.
				api.ServingMetrics(metrics, metricsHandler),
			)

			if !web.Built() {
				log.Warn("no frontend is embedded in this binary",
					slog.String("hint", "run `make web-build && make build`"),
				)
			}

			/*
				Background work.

				Started after the server is built and before it runs, so a
				process that fails to construct one never claims leadership.
				River elects a leader across every process sharing the metadata
				database and only the leader schedules, which is what keeps two
				Pivots from both syncing the same connection.
			*/
			runner := startJobs(ctx, db, repos, log)
			defer stopJobs(runner, log)

			/*
				The query supervisor, which carries a kill from wherever it was
				issued to this process if this process is the one running the
				query.

				Its own pool, because on SQLite the store's is one connection
				by design and a ticker on it would sit between every request
				and the database. Its own failure policy too: a supervisor that
				cannot start means queries here cannot be stopped from
				elsewhere, which is worth saying loudly and is not worth
				refusing to serve over.
			*/
			stopSupervisor := startQuerySupervisor(ctx, db, log)
			defer stopSupervisor()

			return srv.Run(ctx)
		},
	}
}

/*
startQuerySupervisor runs the loop that delivers kills to this process.

Returns its teardown, which closes the pool it opened. A nil-safe no-op when
anything could not be built, so the caller has one shape to defer rather than a
condition.

The owner token is minted here and handed to both halves -- the executor that
stamps it on rows and the supervisor that claims them -- because they must be
the same value and there is no reason for it to exist anywhere else.
*/
func startQuerySupervisor(ctx context.Context, db *store.DB, log *slog.Logger) func() {
	sibling, err := db.SiblingStore()
	if err != nil {
		log.Warn("queries on this instance cannot be stopped from elsewhere",
			logging.Err(err))

		return func() {}
	}

	// One connection: this is two statements on a timer, and on SQLite every
	// extra connection is contention somebody's request pays for.
	sibling.SetMaxOpenConns(1)

	supervisor := query.NewSupervisor(
		QueryOwner, QueryMonitor, repo.New(sibling).System(), log)

	go supervisor.Run(ctx)

	return func() { _ = sibling.Close() }
}

/*
QueryOwner and QueryMonitor are this process's identity and its registry of
running queries.

Package-level because the executor and the supervisor are constructed in
different places and must agree, and because there is exactly one of each per
process -- which is the definition of the thing they represent. When Part 23
builds the query endpoint it takes these.
*/
var (
	QueryOwner   = query.NewOwner()
	QueryMonitor = query.NewMonitor()
)

// announceSetup tells an operator how to claim an unclaimed instance.
//
// On stdout rather than only in the log, and deliberately: this is the one
// message somebody has to read and act on, and a JSON log line among fifty
// others is not how a person finds a token they need to paste. A claimed
// instance prints nothing at all -- a banner that appears on every restart
// forever is a banner nobody reads.
func announceSetup(
	ctx context.Context, env Env, log *slog.Logger,
	svc *setup.Service, token string, cfg *config.Config,
) {
	status, err := svc.Status(ctx)
	if err != nil {
		// Never fatal. This is a banner, and an instance that refuses to serve
		// because it could not decide whether to print one is worse than one
		// that prints nothing -- most obviously when the schema is behind,
		// which `pivot serve` deliberately warns about rather than refusing.
		// That regression is what this comment is here to stop happening
		// again: it was caught by the test that asserts serve keeps running.
		log.Warn("could not tell whether this Pivot has been set up", logging.Err(err))

		return
	}

	if status.Initialized {
		return
	}

	url := cfg.Server.BaseURL
	if url == "" {
		url = fmt.Sprintf("http://localhost:%d", cfg.Server.Port)
	}

	fmt.Fprintf(env.Stdout, `
  This Pivot has no administrator yet.

    Open:  %s/setup
    Token: %s

  The token is what stops somebody else claiming this instance first. It is
  generated per start and stops working the moment setup completes. Set
  PIVOT_SETUP_TOKEN to pin your own.

`, url, token)

	// Also in the log, without the token. An operator scrolling back later
	// should be able to see that the instance was unclaimed at this start
	// without the secret being in a file that outlives the process.
	log.Info("waiting to be set up", slog.String("setup_url", url+"/setup"))
}

func sourceOrNone(path string) string {
	if path == "" {
		return "(none)"
	}

	return path
}

// warnIfBehind reports pending migrations when auto-migration is disabled.
//
// Starting against a stale schema fails later, in a handler, with a confusing
// error. Saying so at startup is cheaper. It is a warning rather than a fatal
// error because a rolling deploy legitimately runs old code against a newer
// schema for a short window.
func warnIfBehind(cmd *cobra.Command, db *store.DB, log *slog.Logger) error {
	statuses, err := store.Status(cmd.Context(), db)
	if err != nil {
		return err
	}

	var pending int

	for _, s := range statuses {
		if !s.Applied {
			pending++
		}
	}

	if pending > 0 {
		log.Warn("pending migrations not applied",
			slog.Int("pending", pending),
			slog.String("hint", "run `pivot migrate up`, or set database.autoMigrate"),
		)
	}

	return nil
}

/*
startJobs builds and starts the background job runner.

Separate from the command body because it is the one piece of startup with a
shutdown of its own -- and because a reader looking for "what runs on a timer"
should find it in one place rather than threaded through two hundred lines of
wiring.
*/
func startJobs(
	ctx context.Context, db *store.DB, repos *repo.Repositories, log *slog.Logger,
) *jobs.Runner {
	workers := river.NewWorkers()
	river.AddWorker(workers, &catalog.SweepWorker{Repos: repos, Log: log})
	river.AddWorker(workers, &catalog.SyncWorker{Repos: repos, Log: log})

	runner, err := jobs.New(db, jobs.Options{
		Workers: workers,
		Periodic: []*river.PeriodicJob{
			river.NewPeriodicJob(
				river.PeriodicInterval(catalog.DefaultInterval),
				func() (river.JobArgs, *river.InsertOpts) { return catalog.SweepArgs{}, nil },
				// Not on start. A restart loop would otherwise sweep every
				// warehouse in the organization on every crash.
				&river.PeriodicJobOpts{RunOnStart: false},
			),
		},
		Logger: log,
	})
	if err != nil {
		return warnNoJobs(log, err)
	}

	if err := runner.Start(ctx); err != nil {
		return warnNoJobs(log, err)
	}

	log.Info("background jobs started",
		slog.String("catalog_sync_interval", catalog.DefaultInterval.String()))

	return runner
}

/*
warnNoJobs reports that background work will not happen, and serves anyway.

Not fatal, deliberately, and this is the second time that lesson has been
learned here: the first-run banner once made `pivot serve` refuse to start when
the schema was behind, and the test that caught it is the same one that caught
this. An instance that will not serve because a *background* feature could not
initialize is worse than one that serves without it -- most obviously when the
cause is that nobody has run `pivot migrate up` yet, which is the case a
warning fixes and a refusal does not.

Loud, though. Silent background work that is not happening is the failure mode
the whole part exists to prevent.
*/
func warnNoJobs(log *slog.Logger, err error) *jobs.Runner {
	log.Warn("background jobs are not running; nothing will be scheduled",
		logging.Err(err),
		slog.String("hint", "run `pivot migrate up`, then restart"),
	)

	return nil
}

// stopJobs drains the runner, giving running jobs a bounded chance to finish.
//
// A background context: the one that brought us here is already canceled by
// the signal that started the shutdown, and a drain given a dead context
// drains nothing.
func stopJobs(runner *jobs.Runner, log *slog.Logger) {
	if runner == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), jobs.DefaultStopTimeout)
	defer cancel()

	if err := runner.Stop(ctx); err != nil {
		log.Warn("the background job runner did not stop cleanly", logging.Err(err))
	}
}
