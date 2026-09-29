package cli

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/Mmd4LIFE/pivot/internal/config"
)

/*
newLimitsCmd shows what bounds querying on this instance.

The limits exist to be met, and a limit somebody meets without being able to
find out what it was is indistinguishable from a bug. Until Part 26 builds the
screens, this is where they are visible -- and it reads the same resolved
configuration the server does, rather than a second copy, so what it prints is
what is enforced.

Each row says where the value came from. "Which of these did I actually set"
is the question somebody has when an instance is not behaving the way they
configured it, and a table of numbers with no provenance cannot answer it.
*/
func newLimitsCmd(env Env, flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "limits",
		Short: "Show the query limits in force on this instance",
		Long: `Print the concurrency, timeout and cache limits querying runs under.

These are the numbers a query meets when it is refused as too busy, cut short
by a timeout, or not cached. Every one of them is settable: the environment
variable that sets it is shown beside it.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := loadConfig(cmd, env, flags)
			if err != nil {
				return err
			}

			printLimits(env, res.Config)

			return nil
		},
	}

	return cmd
}

func printLimits(env Env, cfg *config.Config) {
	q := cfg.Query

	w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)

	fmt.Fprintln(w, "LIMIT\tVALUE\tSET WITH")

	rows := []struct{ name, value, envVar string }{
		{
			"Queries per user, per connection",
			fmt.Sprintf("%d", q.MaxPerUser),
			"PIVOT_QUERY_MAX_PER_USER",
		},
		{
			"Queries per connection, in total",
			fmt.Sprintf("%d", q.MaxPerConnection),
			"PIVOT_QUERY_MAX_PER_CONNECTION",
		},
		{
			"Wait for a slot before refusing",
			time.Duration(q.QueueWait).String(),
			"PIVOT_QUERY_QUEUE_WAIT",
		},
		{
			"Query timeout ceiling",
			orNoCeiling(time.Duration(q.Timeout)),
			"PIVOT_QUERY_TIMEOUT",
		},
		{
			"Result cache",
			onOff(q.Cache.Enabled),
			"PIVOT_QUERY_CACHE_ENABLED",
		},
		{
			"Result cache, total",
			humanBytes(q.Cache.MaxBytes),
			"PIVOT_QUERY_CACHE_MAX_BYTES",
		},
		{
			"Result cache, largest result kept",
			humanBytes(q.Cache.MaxEntryBytes),
			"PIVOT_QUERY_CACHE_MAX_ENTRY_BYTES",
		},
		{
			"Result cache, how long a result is trusted",
			time.Duration(q.Cache.TTL).String(),
			"PIVOT_QUERY_CACHE_TTL",
		},
	}

	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\n", row.name, row.value, row.envVar)
	}

	_ = w.Flush()

	// The two sentences somebody needs after reading the table, because the
	// numbers alone do not say which way each one fails.
	fmt.Fprintf(env.Stdout, "\nA query over a concurrency limit waits up to %s and is then refused.\n",
		time.Duration(q.QueueWait))
	fmt.Fprintln(env.Stdout,
		"A result larger than the cache's per-result limit is streamed and not cached at all.")
}

// orNoCeiling renders a zero timeout as what it means rather than as "0s",
// which reads like "no time at all" and means the opposite.
func orNoCeiling(d time.Duration) string {
	if d <= 0 {
		return "none (each connection's own)"
	}

	return d.String()
}

func onOff(on bool) string {
	if on {
		return "on"
	}

	return "off"
}

// humanBytes renders a byte budget the way an operator wrote it.
func humanBytes(n int64) string {
	const unit = 1024

	if n < unit {
		return fmt.Sprintf("%d B", n)
	}

	div, exp := int64(unit), 0

	for size := n / unit; size >= unit; size /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.0f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
