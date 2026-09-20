package cli

import (
	"log/slog"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/Mmd4LIFE/pivot/internal/api"
	"github.com/Mmd4LIFE/pivot/internal/logging"
	"github.com/Mmd4LIFE/pivot/internal/version"
)

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

			// NotifyContext cancels on the first signal and restores default
			// behavior on the second, so an impatient operator can still
			// force-quit a hung drain with a second Ctrl-C.
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			srv := api.New(cfg.Server, log)

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
