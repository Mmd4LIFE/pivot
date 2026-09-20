package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Mmd4LIFE/pivot/internal/version"
)

func newVersionCmd(env Env) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print build information",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			info := version.Get()

			if asJSON {
				enc := json.NewEncoder(env.Stdout)
				enc.SetIndent("", "  ")

				return enc.Encode(info)
			}

			_, err := fmt.Fprintln(env.Stdout, info.String())

			return err
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "Print as JSON")

	return cmd
}
