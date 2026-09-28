package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Mmd4LIFE/pivot/internal/catalog"
	"github.com/Mmd4LIFE/pivot/internal/connectors"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

/*
`pivot admin sync-catalog`.

The way to run a sync until Part 19-c builds a job runner to do it on a
schedule. It is not a stopgap: somebody has just changed a schema and wants the
catalog to know, and a command that answers that is worth having whatever else
runs on a timer.

The output is the diff rather than a success message. "Synced." tells nobody
anything; "changed public.orders.total: type numeric became text" is the
sentence that makes somebody go and look at a dashboard.
*/

// newSyncCatalogCmd builds `pivot admin sync-catalog`.
func newSyncCatalogCmd(env Env, flags *globalFlags) *cobra.Command {
	var orgSlug string

	cmd := &cobra.Command{
		Use:   "sync-catalog <slug>",
		Short: "Read a connection's schema and record what changed",
		Long: `Read a connection's schema and reconcile it with what Pivot already knew.

Nothing is replaced. Every table and column the source reports is updated in
place, anything it no longer reports is marked gone rather than deleted, and
what differs is printed.

A table marked gone keeps its history and anything pointing at it, because a
source drops a table for reasons that are not "somebody dropped it" -- a
permissions change, or a migration caught halfway.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, repos, err := openRepos(cmd, env, flags)
			if err != nil {
				return err
			}

			defer func() { _ = db.Close() }()

			orgID, _, err := resolveOrg(cmd, repos, orgSlug)
			if err != nil {
				return err
			}

			scope, err := tenant.NewSystemScope(orgID)
			if err != nil {
				return err
			}

			ctx := tenant.WithScope(cmd.Context(), scope)

			conn, err := repos.Connections.GetBySlug(ctx, args[0])
			if err != nil {
				if errors.Is(err, repo.ErrNotFound) {
					return fmt.Errorf("no connection with slug %q", args[0])
				}

				return err
			}

			connector, err := connectors.Open(configFor(conn))
			if err != nil {
				return err
			}

			defer func() { _ = connector.Close() }()

			report, err := catalog.NewSyncer(repos.Catalog).Sync(ctx, conn.ID, connector)
			if err != nil {
				return fmt.Errorf("sync %s: %w", conn.Slug, err)
			}

			fmt.Fprintln(env.Stdout, report.Summary())

			if report.Unchanged() {
				fmt.Fprintln(env.Stdout, "Nothing changed since the last sync.")

				return nil
			}

			fmt.Fprintln(env.Stdout)

			for _, change := range report.Changes {
				fmt.Fprintf(env.Stdout, "  %s\n", change)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&orgSlug, "org", "", "Organization slug; omit when only one exists")

	return cmd
}
