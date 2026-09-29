package query

import (
	"time"

	"github.com/Mmd4LIFE/pivot/internal/config"
)

/*
NewGovernorFrom builds the governor an operator configured.

One place that turns settings into a governor, rather than each caller reading
the fields it happens to know about. A setting that is bound, documented and
printed by `pivot admin limits` but never reaches the thing enforcing it is
worse than no setting at all -- it is a promise the product does not keep, and
the only way it gets noticed is somebody meeting a limit they thought they had
raised.
*/
func NewGovernorFrom(cfg config.QueryConfig) *Governor {
	return NewGovernor(
		WithPerUser(cfg.MaxPerUser),
		WithPerConnection(cfg.MaxPerConnection),
		WithQueueWait(time.Duration(cfg.QueueWait)),
	)
}

/*
NewCacheFrom builds the result cache an operator configured, or nil if they
turned it off.

Nil rather than a cache with a zero budget. An executor given no cache reports
every query as uncached and asks the source, which is exactly what "off" should
mean; a cache that exists and can hold nothing would report misses forever and
look broken rather than disabled.
*/
func NewCacheFrom(cfg config.CacheConfig) *Cache {
	if !cfg.Enabled {
		return nil
	}

	return NewCache(
		WithMaxBytes(cfg.MaxBytes),
		WithMaxEntryBytes(cfg.MaxEntryBytes),
		WithTTL(time.Duration(cfg.TTL)),
	)
}
