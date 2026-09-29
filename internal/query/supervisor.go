package query

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

// How often the supervisor does its two jobs.
const (
	/*
		PollInterval is how often this process looks for kill requests aimed at
		its own queries.

		A second, which is the latency an administrator pressing a button sees
		on top of whatever the source takes to stop. Polling rather than a push
		because the only push available is PostgreSQL's LISTEN/NOTIFY, and a
		kill path that works one way on one engine and another way on the other
		is two paths to keep correct for a saving nobody can perceive.
	*/
	PollInterval = time.Second

	// HeartbeatInterval is how often this process says its running queries are
	// still its own.
	HeartbeatInterval = 5 * time.Second

	/*
		StaleAfter is how long a query may go without a heartbeat before a
		reader calls it abandoned.

		Six missed beats. Sized to swamp ordinary scheduling delay and GC
		pauses rather than to be tight, because the cost of being wrong in one
		direction is a listing that says "abandoned" about a healthy query --
		and somebody then goes looking for a process that is fine.
	*/
	StaleAfter = 30 * time.Second
)

/*
Supervisor carries kills from the database to the process holding the query.

This is the whole of "a kill issued on one instance reaches a query running on
another", and it is deliberately small. The killing itself is not here: it is
[Monitor.Kill], which cancels the context the pipeline handed the connector --
the path Part 20-b measured actually stopping a query at the source. All this
does is notice that somebody asked.

Two loops on one ticker budget. The poll asks whether any query this process
owns has been asked to stop. The heartbeat says the ones it owns are still
being run by something alive, which is what lets a reader tell a query that is
still going from one whose process died.

Both run on a pool of their own. On SQLite the store's pool is a single
connection by design, and a ticker on it would sit between every request and
the database.
*/
type Supervisor struct {
	owner   string
	monitor *Monitor
	system  *repo.SystemRepo
	log     *slog.Logger

	poll      time.Duration
	heartbeat time.Duration
}

// SupervisorOption configures a Supervisor.
type SupervisorOption func(*Supervisor)

// WithIntervals sets how often the two loops run. For tests, which cannot wait
// seconds to find out whether a kill arrived.
func WithIntervals(poll, heartbeat time.Duration) SupervisorOption {
	return func(s *Supervisor) {
		if poll > 0 {
			s.poll = poll
		}

		if heartbeat > 0 {
			s.heartbeat = heartbeat
		}
	}
}

// NewSupervisor builds the loop for one process.
func NewSupervisor(
	owner string, monitor *Monitor, system *repo.SystemRepo, log *slog.Logger,
	opts ...SupervisorOption,
) *Supervisor {
	s := &Supervisor{
		owner:     owner,
		monitor:   monitor,
		system:    system,
		log:       log,
		poll:      PollInterval,
		heartbeat: HeartbeatInterval,
	}

	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

/*
Run works until the context ends.

Returns nothing. A supervisor that could fail its caller would make `serve`
refuse to start over a background feature, which this project has already got
wrong twice -- the note on [warnNoJobs] in the serve command records it. A
database it cannot reach is logged and retried on the next tick.
*/
func (s *Supervisor) Run(ctx context.Context) {
	if s.owner == "" || s.monitor == nil || s.system == nil {
		s.log.Warn("the query supervisor is not configured; queries cannot be stopped from elsewhere")

		return
	}

	polls := time.NewTicker(s.poll)
	defer polls.Stop()

	beats := time.NewTicker(s.heartbeat)
	defer beats.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-polls.C:
			s.deliverKills(ctx)

		case <-beats.C:
			s.beat(ctx)
		}
	}
}

/*
deliverKills stops any of this process's queries that somebody has asked to
stop.

Skipped entirely when this process is running nothing, so an idle instance
issues no statements at all -- which matters on SQLite, where every statement
is a turn on the one connection somebody else's request is waiting for.
*/
func (s *Supervisor) deliverKills(ctx context.Context) {
	if s.monitor.Running() == 0 {
		return
	}

	ids, err := s.system.CancelRequestedFor(ctx, s.owner)
	if err != nil {
		// Logged and retried next tick. A kill that could not be read is a
		// kill that has not happened yet, not one that failed.
		s.log.Warn("could not read kill requests", "error", err)

		return
	}

	for _, id := range ids {
		if s.monitor.Kill(id) {
			s.log.Info("stopped a query at an administrator's request", "query", id)
		}
	}
}

// beat marks this process's running queries as still being run by something
// alive.
func (s *Supervisor) beat(ctx context.Context) {
	if s.monitor.Running() == 0 {
		return
	}

	if _, err := s.system.Heartbeat(ctx, s.owner); err != nil {
		s.log.Warn("could not record a heartbeat", "error", err)
	}
}

/*
NewOwner mints the token that names this process.

A random uuid rather than a hostname or a pid, both of which are reused: a
restarted pod inherits its predecessor's name, and would inherit its abandoned
rows -- so a query that died with the old process would look alive under the
new one, which is the one thing the heartbeat exists to prevent.
*/
func NewOwner() string { return uuid.NewString() }

/*
IsStale reports whether a running row's owner has gone quiet.

Takes the reading time rather than calling the clock, so a caller comparing
many rows compares them all against one moment -- and so a test can ask the
question without waiting.

A row with no heartbeat falls back to when it started, which makes a query that
was running when this version was deployed read as stale once it passes the
threshold. That is the truth: nothing is beating for it.
*/
func IsStale(heartbeat, startedAt, now time.Time) bool {
	last := heartbeat
	if last.IsZero() {
		last = startedAt
	}

	return now.Sub(last) > StaleAfter
}
