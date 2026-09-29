package config_test

import (
	"testing"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/config"
	"github.com/Mmd4LIFE/pivot/internal/query"
)

/*
The configured defaults are the same numbers the query package uses on its own.

They are written twice because they have to be: internal/query depends on the
store and the store depends on internal/config, so config cannot import query
and reference the constants directly. This test is the join. Without it the two
drift, and the drift is invisible -- an instance configured by file behaves one
way and one constructed in a test behaves another, which is the worst kind of
disagreement to debug.

An external test package, so the import that the production code cannot make is
made here, where it costs nothing.
*/
func TestTheQueryDefaultsMatchTheQueryPackage(t *testing.T) {
	t.Parallel()

	c := config.Default().Query

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"max per user", c.MaxPerUser, query.DefaultMaxPerUser},
		{"max per connection", c.MaxPerConnection, query.DefaultMaxPerConnection},
		{"queue wait", time.Duration(c.QueueWait), query.DefaultQueueWait},
		{"cache max bytes", c.Cache.MaxBytes, query.DefaultMaxBytes},
		{"cache max entry bytes", c.Cache.MaxEntryBytes, query.DefaultMaxEntryBytes},
		{"cache ttl", time.Duration(c.Cache.TTL), query.DefaultTTL},
	}

	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("%s: config says %v, internal/query says %v", check.name, check.got, check.want)
		}
	}
}
