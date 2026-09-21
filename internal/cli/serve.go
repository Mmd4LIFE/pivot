package cli

import (
	"log/slog"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/Mmd4LIFE/pivot/internal/api"
	"github.com/Mmd4LIFE/pivot/internal/auth"
	"github.com/Mmd4LIFE/pivot/internal/logging"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/version"
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
			log := logging.New(cfg.Log, env.Stderr)
			slog.SetDefault(log)

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
			} else if err := warnIfBehind(cmd, db, log); err != nil {
				return err
			}

			// NotifyContext cancels on the first signal and restores default
			// behavior on the second, so an impatient operator can still
			// force-quit a hung drain with a second Ctrl-C.
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			repos := repo.New(db)
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

			srv := api.New(cfg.Server, log,
				api.WithCheck(api.Check{
					Name: "database",
					Func: db.HealthCheck,
				}),
				api.WithAuth(api.NewAuthHandler(authSvc, repos, api.CookieConfig{
					Name:   cfg.Auth.CookieName,
					Path:   "/",
					Domain: cfg.Auth.CookieDomain,
					Secure: cfg.Auth.CookieSecure,
				}, log)),
			)

			return srv.Run(ctx)
		},
	}
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
