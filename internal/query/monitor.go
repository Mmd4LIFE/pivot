package query

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

/*
Monitor holds the cancel for every query this process is running.

It is what makes a kill possible at all, and it is deliberately the *only*
thing that kills: canceling the context the pipeline handed to
[connectors.Stream] is the path Part 20-b already proved reaches the source, so
a kill is not a new mechanism to get right, it is the existing one reached from
somewhere else.

Per process, because a context cannot leave one. Whatever carries a kill across
a process boundary ends here, in the instance that owns the query, calling the
same function a caller pressing Ctrl-C would.

The zero value is usable.
*/
type Monitor struct {
	mu      sync.Mutex
	running map[uuid.UUID]context.CancelCauseFunc
}

// NewMonitor returns an empty monitor.
func NewMonitor() *Monitor {
	return &Monitor{running: make(map[uuid.UUID]context.CancelCauseFunc)}
}

/*
watch registers a query's cancel under its log id, and returns the function
that both cancels and forgets it.

Keyed on the log id rather than on anything in memory, because the log id is
the only name for a query that exists outside this process -- it is what an
administrator on another instance will have, and what the query log row is.
*/
func (m *Monitor) watch(id uuid.UUID, cancel context.CancelCauseFunc) func() {
	if m == nil {
		return func() {}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running == nil {
		m.running = make(map[uuid.UUID]context.CancelCauseFunc)
	}

	m.running[id] = cancel

	return func() { m.forget(id) }
}

func (m *Monitor) forget(id uuid.UUID) {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.running, id)
}

/*
Kill stops a query this process is running and reports whether it found one.

False is not a failure. It means the query is not here -- it finished a moment
ago, or it belongs to another instance -- and the caller is the only one who
can tell those apart, because only they know what the log row said.

The cause is attached rather than left as a bare cancellation so that the
pipeline can tell a killed query from one whose caller walked away. Those are
the same signal arriving through the same channel and they are different facts:
one is an administrator's decision and the other is a closed browser tab.
*/
func (m *Monitor) Kill(id uuid.UUID) bool {
	if m == nil {
		return false
	}

	m.mu.Lock()
	cancel, ok := m.running[id]
	m.mu.Unlock()

	if !ok {
		return false
	}

	cancel(ErrKilled)

	return true
}

// Running reports how many queries this process is holding, which is the
// number a test asserts on and an operator compares against the log.
func (m *Monitor) Running() int {
	if m == nil {
		return 0
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.running)
}

// Watching reports whether this process holds a particular query.
func (m *Monitor) Watching(id uuid.UUID) bool {
	if m == nil {
		return false
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	_, ok := m.running[id]

	return ok
}
