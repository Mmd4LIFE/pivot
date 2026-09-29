package query

import (
	"testing"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/config"
)

/*
A configured limit is an enforced limit.

The gap this closes is a specific one and it is easy to leave open: a setting
can be declared, bound to an environment variable, validated, printed by
`pivot admin limits`, and still reach nothing. Every one of those steps passes
its own test while the number does nothing. This is the step that checks the
number arrives.
*/
func TestConfiguredLimitsReachTheGovernor(t *testing.T) {
	t.Parallel()

	cfg := config.Default().Query
	cfg.MaxPerUser = 2
	cfg.MaxPerConnection = 5
	cfg.QueueWait = config.Duration(3 * time.Second)

	perUser, perConnection, wait := NewGovernorFrom(cfg).Limits()

	switch {
	case perUser != 2:
		t.Errorf("per user = %d, want 2", perUser)
	case perConnection != 5:
		t.Errorf("per connection = %d, want 5", perConnection)
	case wait != 3*time.Second:
		t.Errorf("queue wait = %v, want 3s", wait)
	}
}

// And the cache's budget arrives, checked by refusing an entry one byte over
// it -- which is the behavior the number is for.
func TestConfiguredLimitsReachTheCache(t *testing.T) {
	t.Parallel()

	cfg := config.Default().Query.Cache
	cfg.MaxBytes = 1000
	cfg.MaxEntryBytes = 100

	cache := NewCacheFrom(cfg)
	if cache == nil {
		t.Fatal("an enabled cache was not built")
	}

	if got := cache.MaxEntryBytes(); got != 100 {
		t.Errorf("MaxEntryBytes = %d, want 100", got)
	}

	if cache.Put(t.Context(), "k", testColumns(), [][]any{{"x"}}, false, 101) {
		t.Error("an entry over the configured per-entry budget was accepted")
	}

	if !cache.Put(t.Context(), "k", testColumns(), [][]any{{"x"}}, false, 100) {
		t.Error("an entry exactly at the configured budget was refused")
	}
}

// Turning the cache off produces no cache, not an empty one.
func TestADisabledCacheIsNoCache(t *testing.T) {
	t.Parallel()

	cfg := config.Default().Query.Cache
	cfg.Enabled = false

	if NewCacheFrom(cfg) != nil {
		t.Error("a disabled cache was built anyway")
	}
}
