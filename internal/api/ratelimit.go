package api

import (
	"net/http"
	"sync"
	"time"
)

// RateLimit describes an allowance.
type RateLimit struct {
	// Rate is the sustained requests per second.
	Rate float64

	// Burst is how many requests may arrive at once before throttling starts.
	// A burst of 1 makes every client feel rate-limited even at low volume,
	// because real traffic is bursty.
	Burst int
}

// Limits for the endpoint classes. They differ because the risk differs: a
// read is cheap and a login attempt is an attack surface.
//
// Authentication limits are the security-relevant ones, and Part 6 adds the
// per-account lockout that complements them. Per-IP alone is not sufficient
// against a distributed attack, which is why it is only half the defense.
var (
	// LimitDefault applies to ordinary API traffic.
	LimitDefault = RateLimit{Rate: 20, Burst: 40}

	// LimitAuth applies to authentication endpoints.
	LimitAuth = RateLimit{Rate: 0.5, Burst: 5}

	// LimitExpensive applies to query execution and exports, from Phase 1.
	LimitExpensive = RateLimit{Rate: 2, Burst: 5}

	// LimitTelemetry applies to browser error reports.
	//
	// The burst is what matters: one broken render can fire a render error, an
	// onerror and a rejected promise within the same frame, and a limit that
	// only let the first one through would hide the two that explain it. The
	// sustained rate is low because the endpoint is unauthenticated and writes
	// to the log -- an unbounded one is a way to fill somebody's disk from a
	// browser tab.
	LimitTelemetry = RateLimit{Rate: 0.2, Burst: 10}
)

// bucket is a token bucket.
//
// Hand-rolled rather than pulling golang.org/x/time/rate: the whole thing is
// thirty lines, and it keeps the dependency list short — which matters on a
// network where a single added module has repeatedly been the slowest part of
// a build.
type bucket struct {
	tokens   float64
	lastSeen time.Time
}

// Limiter throttles by key.
//
// In-process only, which is the right scope for now and explicitly wrong for
// multi-node: N replicas each allow the full rate, so the effective limit is
// N times the configured one. Phase 9 moves this to Valkey; until then the
// limit is a guard against accidents and casual abuse, not a hard quota.
type Limiter struct {
	limit RateLimit
	now   func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket

	// lastSweep bounds memory: without eviction, a scan across many source
	// addresses grows the map without limit, which turns a rate limiter into
	// a memory-exhaustion vector.
	lastSweep time.Time
}

// sweepInterval and bucketTTL control eviction of idle buckets.
const (
	sweepInterval = 5 * time.Minute
	bucketTTL     = 10 * time.Minute
)

// NewLimiter builds a limiter.
func NewLimiter(limit RateLimit) *Limiter {
	return &Limiter{
		limit:   limit,
		now:     time.Now,
		buckets: make(map[string]*bucket),
	}
}

// Allow reports whether a request for key may proceed, and if not, how long to
// wait before retrying.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweepLocked(now)

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: float64(l.limit.Burst), lastSeen: now}
		l.buckets[key] = b
	}

	// Refill for elapsed time, capped at the burst size.
	elapsed := now.Sub(b.lastSeen).Seconds()
	b.tokens = min(b.tokens+elapsed*l.limit.Rate, float64(l.limit.Burst))
	b.lastSeen = now

	if b.tokens >= 1 {
		b.tokens--

		return true, 0
	}

	// Time until one whole token is available.
	deficit := 1 - b.tokens
	wait := time.Duration(deficit / l.limit.Rate * float64(time.Second))

	return false, wait
}

// sweepLocked drops buckets nobody has touched recently.
func (l *Limiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSweep) < sweepInterval {
		return
	}

	l.lastSweep = now

	for key, b := range l.buckets {
		if now.Sub(b.lastSeen) > bucketTTL {
			delete(l.buckets, key)
		}
	}
}

// Len reports the number of tracked keys, for tests and metrics.
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.buckets)
}

// KeyFunc derives the rate-limit key for a request.
type KeyFunc func(*http.Request) string

// KeyByIP limits per client address.
func KeyByIP(r *http.Request) string { return clientIP(r) }

// KeyByIPAndPath limits per address and path, so a client hammering one
// endpoint does not exhaust its allowance for every other one.
func KeyByIPAndPath(r *http.Request) string { return clientIP(r) + " " + r.URL.Path }

// WithRateLimit throttles requests.
//
// A throttled response carries Retry-After, because a client that does not
// know when to retry will either give up or spin — and the spinning ones are
// what turn a throttle into an outage.
func WithRateLimit(limiter *Limiter, key KeyFunc) Middleware {
	if key == nil {
		key = KeyByIP
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed, wait := limiter.Allow(key(r))
			if !allowed {
				seconds := int64(wait.Seconds())
				if seconds < 1 {
					seconds = 1
				}

				w.Header().Set("Retry-After", itoa64(seconds))

				WriteError(w, r, &APIError{
					Code:    CodeRateLimited,
					Message: CodeRateLimited.Summary(),
				})

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
