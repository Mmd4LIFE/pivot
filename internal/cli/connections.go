package cli

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

/*
Connections, from the command line.

The browser gets these screens in Part 26. They exist here first for the same
reason `admin add-provider` does: an organization has to be able to configure
its first connection, and the administrator who would do it in the UI may be
looking at an instance that has no data in it yet.

The password comes from PIVOT_CONNECTION_PASSWORD rather than a flag, because a
password passed as an argument lands in the shell history and in the process
list, where any other account on the machine can read it.
*/

// newAddConnectionCmd builds `pivot admin add-connection`.
func newAddConnectionCmd(env Env, flags *globalFlags) *cobra.Command {
	var (
		slug, name, kind, description, orgSlug string
		host, database, username, sslMode      string
		port                                   int
		maxOpenConns, queryTimeout             int
		maxRows                                int64
		noTest                                 bool
	)

	cmd := &cobra.Command{
		Use:   "add-connection",
		Short: "Configure a data source for an organization",
		Long: `Configure a data source an organization can query.

The password is read from PIVOT_CONNECTION_PASSWORD rather than a flag: a
password passed as an argument lands in the shell history and in the process
list. It is stored encrypted, so a copy of the database without the key cannot
reveal it.

The connection is tested before it is stored, and a connection that cannot be
reached is refused -- a stored connection that has never worked is a support
ticket waiting to happen. Pass --no-test to store it anyway, which is what a
source behind a firewall this machine cannot cross needs. The configuration is
still checked: --no-test skips reaching the database, not validating it.

What a connection needs depends on the connector. A server takes --db-host,
--database and --username; a file-backed one such as sqlite takes the path as
--database and nothing else.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Only what Pivot itself needs. What a *connection* needs is the
			// dialect's business and is checked below by opening it: a
			// PostgreSQL wants a host and a username, and a SQLite file has
			// neither. Requiring them here meant a file-backed connector could
			// not be configured at all.
			for flag, value := range map[string]string{"--slug": slug, "--name": name} {
				if strings.TrimSpace(value) == "" {
					return fmt.Errorf("%s is required", flag)
				}
			}

			if !slices.Contains(connectors.Kinds(), connectors.Kind(kind)) {
				return fmt.Errorf("unknown connector %q; this build has %s",
					kind, kindNames())
			}

			db, repos, err := openRepos(cmd, env, flags)
			if err != nil {
				return err
			}

			defer func() { _ = db.Close() }()

			orgID, orgName, err := resolveOrg(cmd, repos, orgSlug)
			if err != nil {
				return err
			}

			scope, err := tenant.NewSystemScope(orgID)
			if err != nil {
				return err
			}

			ctx := tenant.WithScope(cmd.Context(), scope)

			lookup := env.Lookup
			if lookup == nil {
				lookup = os.LookupEnv
			}

			password, _ := lookup("PIVOT_CONNECTION_PASSWORD")

			cfg := connectors.Config{
				Kind: connectors.Kind(kind), Host: host, Port: port,
				Database: database, Username: username, Password: password,
				SSLMode: sslMode, MaxOpenConns: maxOpenConns,
				QueryTimeoutSeconds: queryTimeout, MaxRows: maxRows,
			}

			/*
				Opened before it is stored, and tested unless told not to.

				Open validates without dialing, so it runs even under
				--no-test: the escape hatch is for a source this machine
				cannot reach, not for storing a configuration the connector
				would refuse. A row that can never be opened is a support
				ticket with a delay on it.
			*/
			connector, err := connectors.Open(cfg)
			if err != nil {
				return err
			}

			defer func() { _ = connector.Close() }()

			// Tested while the person who typed the hostname is still here,
			// rather than reported to whoever opens the query editor tomorrow.
			if !noTest {
				if terr := connector.Test(cmd.Context()); terr != nil {
					return fmt.Errorf(
						"could not reach that database: %w\n\n"+
							"Nothing was stored. Pass --no-test to store it anyway", terr)
				}

				fmt.Fprintln(env.Stdout, "Connected.")
			}

			conn, err := repos.Connections.Create(ctx, repo.CreateConnection{
				Slug: slug, Name: name, Kind: kind, Description: description,
				Host: host, Port: int64(port), Database: database,
				Username: username, Password: password, SSLMode: sslMode,
				MaxOpenConns:        int64(maxOpenConns),
				MaxRows:             maxRows,
				QueryTimeoutSeconds: int64(queryTimeout),
				IsEnabled:           true,
			})
			if err != nil {
				if errors.Is(err, repo.ErrDuplicate) {
					return fmt.Errorf("a connection with slug %q already exists in %q", slug, orgName)
				}

				return err
			}

			if !noTest {
				// Recorded after the row exists, so the list page can show the
				// result without anybody testing again.
				if rerr := repos.Connections.RecordTest(ctx, conn.ID, nil); rerr != nil {
					return rerr
				}
			}

			fmt.Fprintf(env.Stdout, "Configured %s (%s) in organization %s.\n",
				conn.Name, conn.Slug, orgName)

			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&slug, "slug", "", "URL segment for this connection (required)")
	f.StringVar(&name, "name", "", "Display name (required)")
	f.StringVar(&kind, "kind", string(connectors.KindPostgres),
		"Connector to use ("+kindNames()+")")
	f.StringVar(&description, "description", "", "What this connection is for")
	f.StringVar(&orgSlug, "org", "", "Organization slug; omit when only one exists")
	// --db-host and --db-port rather than --host and --port, because the root
	// command already has persistent flags by those names for Pivot's own
	// listener. A local flag shadows the persistent one, which made
	// `--port 5433` set the *server* port to zero and fail validation before
	// the command ran -- a collision worth a clearer name rather than a
	// clever fix.
	f.StringVar(&host, "db-host", "", "Hostname of the database server; not used by a file-backed connector")
	f.IntVar(&port, "db-port", 0, "Port on the database server; zero uses the connector's default")
	f.StringVar(&database, "database", "", "Database name, or the file path for a file-backed connector")
	f.StringVar(&username, "username", "", "Username; not used by a file-backed connector")
	f.StringVar(&sslMode, "ssl-mode", "", "TLS mode; empty uses the connector's default")
	f.IntVar(&maxOpenConns, "max-open-conns", 0, "Pool size; zero uses the default")
	f.IntVar(&queryTimeout, "query-timeout", 0, "Per-query timeout in seconds; zero uses the default")
	f.Int64Var(&maxRows, "max-rows", 0, "Row cap for a result; zero uses the default")
	f.BoolVar(&noTest, "no-test", false, "Store the connection without reaching the database first")

	return cmd
}

// newTestConnectionCmd builds `pivot admin test-connection`.
func newTestConnectionCmd(env Env, flags *globalFlags) *cobra.Command {
	var orgSlug string

	cmd := &cobra.Command{
		Use:   "test-connection <slug>",
		Short: "Check that a stored connection still works",
		Long: `Reach a stored connection's database and report what happened.

The result is recorded on the connection, so a list page can show it without
testing every row -- which would make rendering that page a thundering herd
against every warehouse in the organization.`,
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

			testErr := testConfig(cmd, configFor(conn))

			// Recorded whether it worked or not: a failure is the more useful
			// of the two to have on record.
			if rerr := repos.Connections.RecordTest(ctx, conn.ID, testErr); rerr != nil {
				return rerr
			}

			if testErr != nil {
				return fmt.Errorf("%s (%s): %w", conn.Name, conn.Slug, testErr)
			}

			fmt.Fprintf(env.Stdout, "%s (%s): connected.\n", conn.Name, conn.Slug)

			return nil
		},
	}

	cmd.Flags().StringVar(&orgSlug, "org", "", "Organization slug; omit when only one exists")

	return cmd
}

// newListConnectionsCmd builds `pivot admin list-connections`.
func newListConnectionsCmd(env Env, flags *globalFlags) *cobra.Command {
	var orgSlug string

	cmd := &cobra.Command{
		Use:   "list-connections",
		Short: "List an organization's data sources",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, repos, err := openRepos(cmd, env, flags)
			if err != nil {
				return err
			}

			defer func() { _ = db.Close() }()

			orgID, orgName, err := resolveOrg(cmd, repos, orgSlug)
			if err != nil {
				return err
			}

			scope, err := tenant.NewSystemScope(orgID)
			if err != nil {
				return err
			}

			conns, err := repos.Connections.List(tenant.WithScope(cmd.Context(), scope))
			if err != nil {
				return err
			}

			if len(conns) == 0 {
				fmt.Fprintf(env.Stdout, "No connections in %s yet.\n", orgName)

				return nil
			}

			w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)

			fmt.Fprintln(w, "SLUG\tNAME\tKIND\tTARGET\tENABLED\tLAST TEST")

			for _, c := range conns {
				// No password, and no field that could hold one. This output
				// ends up in issues and screenshots.
				fmt.Fprintf(w, "%s\t%s\t%s\t%s@%s:%d/%s\t%t\t%s\n",
					c.Slug, c.Name, c.Kind,
					c.Username, c.Host, c.Port, c.Database,
					bool(c.IsEnabled), describeTest(c))
			}

			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&orgSlug, "org", "", "Organization slug; omit when only one exists")

	return cmd
}

// configFor builds a connector config from a stored row.
//
// The password arrives decrypted, because the repository layer opens it on the
// way out -- above that layer it is simply the password.
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

// testConfig opens a connector, reaches the source, and closes it again.
func testConfig(cmd *cobra.Command, cfg connectors.Config) error {
	connector, err := connectors.Open(cfg)
	if err != nil {
		return err
	}

	defer func() { _ = connector.Close() }()

	return connector.Test(cmd.Context())
}

// describeTest renders the last test result for a list.
func describeTest(c model.Connection) string {
	if !c.LastTestedAt.Valid {
		return "never"
	}

	when := c.LastTestedAt.Time.Format("2006-01-02 15:04")

	if bool(c.LastTestOk) {
		return "ok " + when
	}

	return "FAILED " + when
}

func kindNames() string {
	kinds := connectors.Kinds()

	names := make([]string, 0, len(kinds))
	for _, k := range kinds {
		names = append(names, k.String())
	}

	return strings.Join(names, ", ")
}
