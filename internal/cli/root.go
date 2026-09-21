// Package cli builds Pivot's command tree.
//
// Commands are assembled rather than registered into a package-level global,
// so a test can construct an isolated tree with its own IO and environment.
// Nothing here calls os.Exit; that decision belongs to main.
package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Mmd4LIFE/pivot/internal/config"
)

// Env carries everything a command needs from the outside world. Injecting it
// is what makes the command tree testable without touching the real process
// environment or standard streams.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer

	// Lookup resolves environment variables. Nil means os.LookupEnv.
	Lookup func(string) (string, bool)
}

// globalFlags are the flags shared by every command that loads configuration.
type globalFlags struct {
	configPath string
	host       string
	port       int
	logLevel   string
	logFormat  string
}

// NewRootCmd builds the full command tree.
func NewRootCmd(env Env) *cobra.Command {
	var flags globalFlags

	root := &cobra.Command{
		Use:   "pivot",
		Short: "The open business intelligence platform for the AI era",
		Long: `Pivot is a self-hostable BI platform that unifies data connectivity,
a governed semantic layer, exploration, visualization, dashboards, alerting,
data flows, and an AI analyst into one product.

Configuration precedence, lowest to highest:

  defaults  ->  config file  ->  environment (PIVOT_*)  ->  flags

Run "pivot config show" to see the resolved configuration and where each
value came from.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		// Without a subcommand, show help rather than doing something surprising.
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	root.SetOut(env.Stdout)
	root.SetErr(env.Stderr)

	pf := root.PersistentFlags()
	pf.StringVarP(&flags.configPath, "config", "c", "",
		"Path to a config file (default: search ./pivot.yaml, ./config/pivot.yaml, /etc/pivot/pivot.yaml)")
	pf.StringVar(&flags.host, "host", "", "Interface to bind; empty binds all interfaces")
	pf.IntVarP(&flags.port, "port", "p", 0, "Port to listen on")
	pf.StringVar(&flags.logLevel, "log-level", "",
		fmt.Sprintf("Log level (%s)", joinOptions(config.LogLevels)))
	pf.StringVar(&flags.logFormat, "log-format", "",
		fmt.Sprintf("Log format (%s)", joinOptions(config.LogFormats)))

	root.AddCommand(
		newServeCmd(env, &flags),
		newMigrateCmd(env, &flags),
		newAdminCmd(env, &flags),
		newVersionCmd(env),
		newConfigCmd(env, &flags),
	)

	return root
}

// loadConfig resolves configuration for a command, applying only the flags the
// user actually typed.
//
// Checking Changed() is what makes precedence correct: a flag left at its
// zero value must not outrank an environment variable, and cobra cannot tell
// us that on its own.
func loadConfig(cmd *cobra.Command, env Env, flags *globalFlags) (*config.Result, error) {
	return config.Load(config.Options{
		Path:   flags.configPath,
		Lookup: env.Lookup,
		Override: func(c *config.Config) {
			fs := cmd.Flags()

			if fs.Changed("host") {
				c.Server.Host = flags.host
			}

			if fs.Changed("port") {
				c.Server.Port = flags.port
			}

			if fs.Changed("log-level") {
				c.Log.Level = flags.logLevel
			}

			if fs.Changed("log-format") {
				c.Log.Format = flags.logFormat
			}
		},
	})
}

func joinOptions(opts []string) string {
	out := ""

	for i, o := range opts {
		if i > 0 {
			out += ", "
		}
		out += o
	}

	return out
}
