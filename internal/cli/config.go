package cli

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Mmd4LIFE/pivot/internal/config"
)

func newConfigCmd(env Env, flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newConfigShowCmd(env, flags),
		newConfigEnvCmd(env),
	)

	return cmd
}

func newConfigShowCmd(env Env, flags *globalFlags) *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print the resolved configuration",
		Long: `Print the configuration Pivot would run with, after applying
defaults, the config file, environment variables, and flags in that order.

This is the answer to "why is this value what it is?" — it reports which
config file was used, or that none was found.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := loadConfig(cmd, env, flags)
			if err != nil {
				return err
			}

			switch format {
			case "json":
				enc := json.NewEncoder(env.Stdout)
				enc.SetIndent("", "  ")

				return enc.Encode(res.Config)

			case "yaml":
				fmt.Fprintf(env.Stdout, "# config file: %s\n", sourceOrNone(res.SourceFile))

				enc := yaml.NewEncoder(env.Stdout)
				enc.SetIndent(2)

				if err := enc.Encode(res.Config); err != nil {
					return err
				}

				return enc.Close()

			default:
				return fmt.Errorf("unknown format %q (want json or yaml)", format)
			}
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "yaml", "Output format (yaml, json)")

	return cmd
}

func newConfigEnvCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "env",
		Short: "List supported environment variables",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)

			fmt.Fprintln(w, "VARIABLE\tDESCRIPTION")

			for _, v := range config.EnvVars() {
				fmt.Fprintf(w, "%s\t%s\n", v.Key, v.Help)
			}

			return w.Flush()
		},
	}
}
