package cli

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/Mmd4LIFE/pivot/internal/query"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

/*
newQueriesCmd is the query monitor.

Two questions, and they are asked at different moments. "What is running right
now" is asked while something is wrong, and it is answerable at all because
Part 20-b writes the log row when a query *starts* rather than when it ends --
so this reads the database rather than some process's memory, and a restart
loses nothing.

"What has this cost" is asked afterwards, and is a different shape: an
aggregate over a window rather than a list. Both live here because the person
asking is the same person, and they will not remember two command names.
*/
func newQueriesCmd(env Env, flags *globalFlags) *cobra.Command {
	var (
		orgSlug string
		usage   bool
		since   time.Duration
		kill    string
	)

	cmd := &cobra.Command{
		Use:   "queries",
		Short: "Show what is running now, or what querying has cost",
		Long: `Show the queries running right now, newest first.

--usage shows what each person has cost over a window instead: how many
queries, how long they took in total, how many rows and bytes they moved, how
many failed and how many were served from cache.

--kill asks for a running query to stop. The request is recorded and the
instance actually running the query acts on it, so this works from any
instance and from this command, which is already a different process from the
server.

Running queries are read from the query log rather than from any process's
memory, so this is answerable from any instance and survives a restart.

A running query is shown with its state: beating normally, or last seen some
time ago -- which means whatever was running it is gone, and nothing can
deliver a kill to it.`,
		Args: cobra.NoArgs,
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

			ctx := tenant.WithScope(cmd.Context(), scope)

			if kill != "" {
				return killQuery(ctx, env, repos, kill)
			}

			if usage {
				return printUsage(ctx, env, repos, orgName, since)
			}

			running, err := repos.QueryLog.Running(ctx)
			if err != nil {
				return err
			}

			if len(running) == 0 {
				fmt.Fprintf(env.Stdout, "Nothing is running in %s.\n", orgName)

				return nil
			}

			printRunning(env, running)

			return nil
		},
	}

	cmd.Flags().StringVar(&orgSlug, "org", "", "Organization slug; omit when only one exists")
	cmd.Flags().BoolVar(&usage, "usage", false, "Show per-person usage over a window instead")
	cmd.Flags().DurationVar(&since, "since", 24*time.Hour, "Window for --usage")
	cmd.Flags().StringVar(&kill, "kill", "", "Ask a running query to stop, by id")

	return cmd
}

func printRunning(env Env, running []model.QueryLogEntry) {
	w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)

	fmt.Fprintln(w, "ID\tUSER\tRUNNING FOR\tSTATE\tSQL")

	now := time.Now()

	for _, q := range running {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			q.ID, describeActor(q.UserID),
			now.Sub(q.StartedAt.Time).Truncate(time.Second),
			describeHealth(q, now),
			oneLine(q.SQLText, 50))
	}

	_ = w.Flush()
}

/*
describeHealth says whether anything is still running this query.

Three answers, not two, and the third is why this column exists. A row whose
owner is beating is running. A row whose owner has gone quiet is abandoned --
the process that held it is gone, and no kill can be delivered to it, which is
the thing an administrator most needs to know before they wait for one. And a
row with no owner at all was written before any of this existed, or by a path
with no supervisor; calling that "abandoned" would be inventing a failure.

A kill already asked for is said so, because the next question after pressing
the button is whether it took.
*/
func describeHealth(q model.QueryLogEntry, now time.Time) string {
	state := "running"

	switch {
	case q.Owner == "":
		state = "unknown owner"

	case query.IsStale(lastBeat(q), q.StartedAt.Time, now):
		last := lastBeat(q)
		if last.IsZero() {
			last = q.StartedAt.Time
		}

		state = fmt.Sprintf("abandoned, last seen %s ago", now.Sub(last).Truncate(time.Second))
	}

	if q.CancelRequestedAt.Valid {
		state += fmt.Sprintf(" (kill asked %s ago)",
			now.Sub(q.CancelRequestedAt.Time.Time).Truncate(time.Second))
	}

	return state
}

/*
killQuery asks for a query to stop and says what will happen next.

An ask rather than a do, and the output says so. The instance running the
query is the only thing that can stop it, and this command is deliberately not
that instance -- so reporting "stopped" here would be claiming something this
process cannot know.
*/
func killQuery(ctx context.Context, env Env, repos *repo.Repositories, id string) error {
	queryID, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("%q is not a query id: %w", id, err)
	}

	before, err := repos.QueryLog.Get(ctx, queryID)
	if err != nil {
		return fmt.Errorf("no query %s is on record: %w", id, err)
	}

	if err := repos.QueryLog.RequestCancel(ctx, queryID); err != nil {
		return fmt.Errorf("that query is not running: %w", err)
	}

	fmt.Fprintf(env.Stdout, "Asked query %s to stop.\n", queryID)

	// And the one thing that decides whether the ask will be honored.
	switch {
	case before.Owner == "":
		fmt.Fprintln(env.Stdout,
			"Nothing claims this query, so no instance will act on the request.")

	case query.IsStale(lastBeat(before), before.StartedAt.Time, time.Now()):
		fmt.Fprintln(env.Stdout,
			"Whatever was running it has gone quiet, so the request may never be delivered.")

	default:
		fmt.Fprintln(env.Stdout,
			"The instance running it will stop it within a second or so.")
	}

	return nil
}

func printUsage(
	ctx context.Context, env Env, repos *repo.Repositories, orgName string, since time.Duration,
) error {
	usage, err := repos.QueryLog.UsageByUser(ctx, time.Now().Add(-since))
	if err != nil {
		return err
	}

	if len(usage) == 0 {
		fmt.Fprintf(env.Stdout, "No queries in %s in the last %s.\n", orgName, since)

		return nil
	}

	w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)

	fmt.Fprintln(w, "USER\tQUERIES\tTOTAL TIME\tROWS\tFAILED\tFROM CACHE")

	for _, u := range usage {
		fmt.Fprintf(w, "%s\t%d\t%s\t%d\t%d\t%d\n",
			describeActor(u.UserID), u.Queries,
			(time.Duration(u.TotalMs) * time.Millisecond).String(),
			u.TotalRows, u.Failures, u.CacheHits)
	}

	return w.Flush()
}

// oneLine flattens a statement for a table cell. A SQL statement is
// multi-line by nature and a table row is not.
func oneLine(sql string, width int) string {
	flat := make([]rune, 0, width)

	space := false

	for _, r := range sql {
		if r == '\n' || r == '\t' || r == '\r' || r == ' ' {
			space = true

			continue
		}

		if space && len(flat) > 0 {
			flat = append(flat, ' ')
		}

		space = false

		if len(flat) >= width {
			return string(flat) + "..."
		}

		flat = append(flat, r)
	}

	return string(flat)
}

/*
describeActor names who ran something, or says plainly that nobody did.

"system" rather than a blank cell or a zero uuid: a scheduled refresh has no
person behind it, and a monitor that showed an empty column there would have
somebody looking for the user whose queries these are.
*/
func describeActor(id uuid.NullUUID) string {
	if !id.Valid {
		return "system"
	}

	return id.UUID.String()
}

// lastBeat is the heartbeat as a plain time, or zero when nothing has beaten.
func lastBeat(q model.QueryLogEntry) time.Time {
	if !q.HeartbeatAt.Valid {
		return time.Time{}
	}

	return q.HeartbeatAt.Time.Time
}
