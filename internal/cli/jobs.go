package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/spf13/cobra"

	"github.com/Mmd4LIFE/pivot/internal/jobs"
	"github.com/Mmd4LIFE/pivot/internal/logging"
	"github.com/Mmd4LIFE/pivot/internal/store"
)

/*
`pivot admin jobs`.

The answer to "is the scheduler still running, and did anything fail". Every
job system's real failure mode is that it stops and nothing says so, and the
second-worst is that a job fails repeatedly into a log nobody reads.

So this lists the most recent jobs with their state and, for the ones that
failed, the error -- because a list that shows a red state and makes somebody
go digging for the reason has only moved the problem.
*/

// newJobsCmd builds `pivot admin jobs`.
func newJobsCmd(env Env, flags *globalFlags) *cobra.Command {
	var (
		limit  int
		failed bool
	)

	cmd := &cobra.Command{
		Use:   "jobs",
		Short: "Show recent background jobs and whether they worked",
		Long: `List recent background jobs, newest first.

Background work fails quietly by nature: nobody is waiting for it, so a broken
catalog sync looks exactly like a schema that has not changed. This is how to
tell the difference.

--failed narrows to the ones that are retrying or have given up, which is the
question somebody usually has.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := loadConfig(cmd, env, flags)
			if err != nil {
				return err
			}

			log := logging.New(res.Config.Log, env.Stderr)

			db, err := store.Open(cmd.Context(), res.Config.Database, log)
			if err != nil {
				return err
			}

			defer func() { _ = db.Close() }()

			// A runner with no workers: this only reads.
			runner, err := jobs.New(db, jobs.Options{
				Workers: river.NewWorkers(), Logger: log,
			})
			if err != nil {
				return err
			}

			defer func() {
				if serr := runner.Stop(cmd.Context()); serr != nil {
					log.Debug("closing the job reader", logging.Err(serr))
				}
			}()

			params := river.NewJobListParams().First(limit).OrderBy(river.JobListOrderByTime, river.SortOrderDesc)
			if failed {
				params = params.States(rivertype.JobStateRetryable, rivertype.JobStateDiscarded)
			}

			list, err := runner.Client().JobList(cmd.Context(), params)
			if err != nil {
				return fmt.Errorf("list jobs: %w", err)
			}

			if len(list.Jobs) == 0 {
				if failed {
					fmt.Fprintln(env.Stdout, "No failed jobs.")
				} else {
					fmt.Fprintln(env.Stdout,
						"No jobs yet. They are scheduled by whichever process is serving;"+
							" if none is, nothing runs.")
				}

				return nil
			}

			writeJobs(env, list.Jobs)

			return nil
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 20, "How many to show")
	cmd.Flags().BoolVar(&failed, "failed", false, "Only jobs that are retrying or have given up")

	return cmd
}

// writeJobs prints a job table, with the error under any row that has one.
func writeJobs(env Env, list []*rivertype.JobRow) {
	w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)

	fmt.Fprintln(w, "STATE\tKIND\tATTEMPT\tWHEN")

	for _, job := range list {
		when := job.CreatedAt
		if job.AttemptedAt != nil {
			when = *job.AttemptedAt
		}

		fmt.Fprintf(w, "%s\t%s\t%d/%d\t%s\n",
			job.State, job.Kind, job.Attempt, job.MaxAttempts,
			when.Local().Format(time.RFC3339))
	}

	_ = w.Flush()

	// The errors, after the table rather than squeezed into a column: a
	// driver's message is a paragraph, and a table that truncates it to forty
	// characters is a table that makes somebody go and find the log anyway.
	for _, job := range list {
		if len(job.Errors) == 0 {
			continue
		}

		last := job.Errors[len(job.Errors)-1]

		fmt.Fprintf(env.Stdout, "\n%s (attempt %d): %s\n",
			job.Kind, last.Attempt, strings.TrimSpace(last.Error))
	}
}
