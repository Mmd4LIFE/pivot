package config

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is Pivot's complete runtime configuration.
//
// Defaults must be good enough that `./pivot serve` works with no file, no
// environment, and no flags — that is the 30-second-install commitment from
// docs/vision.md. Every field added here needs a default, a validation rule,
// and an entry in the environment binding table in env.go.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Auth     AuthConfig     `yaml:"auth"`
	Log      LogConfig      `yaml:"log"`

	Observability ObservabilityConfig `yaml:"observability"`
	Setup         SetupConfig         `yaml:"setup"`
	Secrets       SecretsConfig       `yaml:"secrets"`
}

// SecretsConfig controls encryption of stored secrets.
//
// The key is what makes a stolen database useless, so where it lives matters
// more than any setting here: **not beside the database**, and not in the same
// backup. See docs/operations/secrets.md.
type SecretsConfig struct {
	// Key is the master key, base64-encoded, 32 bytes.
	//
	// Supplied directly for container deployments, where a file is awkward and
	// the orchestrator already has a way to deliver a secret. Takes precedence
	// over KeyFile.
	Key string `yaml:"key"`

	// KeyFile is where to read the key from, and where to write one if none
	// exists yet.
	//
	// Empty means `pivot.key` beside the database, which is the zero-config
	// answer: an instance that has never been configured still encrypts, and
	// the database on its own is no longer enough. It is a weaker position
	// than a key held elsewhere, and it is the one a first run can actually
	// take -- refusing to start until somebody provisions a key would make the
	// thirty-second promise a lie.
	KeyFile string `yaml:"keyFile"`

	// PreviousKeys are accepted for decryption but never used to encrypt.
	//
	// This is the middle of a rotation: values written under the old key have
	// to stay readable until `pivot secrets rewrap` has been through them.
	PreviousKeys []string `yaml:"previousKeys"`
}

// SetupConfig controls the first run.
//
// The whole section stops mattering the moment somebody claims the instance,
// which is why it has exactly one setting and no plans for more.
type SetupConfig struct {
	// Token protects the setup endpoint.
	//
	// Empty means one is generated at startup and printed in the banner, which
	// is the right default: without a token, whoever reaches an unclaimed
	// Pivot first becomes its administrator, and "it is only open for a few
	// seconds" is not a security argument for anything on a network.
	//
	// Set it explicitly for automated provisioning, where reading a token back
	// out of a log is not possible.
	Token string `yaml:"token"`
}

// ObservabilityConfig controls tracing.
//
// Off by default, and that default is deliberate: a single-binary install on
// somebody's laptop should not try to reach a collector that does not exist.
// The instrumentation costs nothing when this is off -- OpenTelemetry's no-op
// tracer makes a span a couple of pointer assignments -- so the call sites
// stay unconditional.
type ObservabilityConfig struct {
	Tracing TracingConfig `yaml:"tracing"`
	Metrics MetricsConfig `yaml:"metrics"`
}

// MetricsConfig controls the Prometheus endpoint.
type MetricsConfig struct {
	// Enabled exposes /metrics.
	//
	// Unlike tracing this defaults to *on*. It needs no collector to exist, it
	// costs nothing until something scrapes it, and an operator who has to go
	// and enable metrics before they can find out why the thing is slow has
	// been failed already.
	Enabled bool `yaml:"enabled"`
}

// TracingConfig configures OpenTelemetry export.
type TracingConfig struct {
	// Enabled turns tracing on. Everything below is ignored when it is false.
	Enabled bool `yaml:"enabled"`

	// Endpoint is the collector's OTLP/HTTP address: host and port, no scheme
	// and no path. The exporter appends /v1/traces itself.
	Endpoint string `yaml:"endpoint"`

	// Insecure sends over plain HTTP. Right for a collector running alongside
	// Pivot in the same pod or host; wrong for anything crossing a network.
	Insecure bool `yaml:"insecure"`

	// SampleRatio is the fraction of traces kept, 0 to 1.
	//
	// Sampling is parent-respecting, so a request that arrives already sampled
	// stays sampled: a trace is never half-recorded, which would be worse than
	// not recording it at all.
	SampleRatio float64 `yaml:"sampleRatio"`

	// ServiceName identifies this process to the collector. Several Pivots
	// reporting as the same name is how a trace becomes unreadable.
	ServiceName string `yaml:"serviceName"`
}

// DatabaseConfig points Pivot at its metadata store.
type DatabaseConfig struct {
	// URL is the metadata database. Recognized forms:
	//
	//	pivot.db                       SQLite file (the default)
	//	sqlite://data/pivot.db         SQLite file, explicit
	//	:memory:                       SQLite, in-memory (tests)
	//	postgres://user:pw@host/db     PostgreSQL
	//
	// A bare path with no scheme is treated as a SQLite file, so the
	// zero-config first run needs no URL at all.
	URL string `yaml:"url"`

	// MaxOpenConns caps concurrent connections. Zero means the engine default:
	// 1 for SQLite, 25 for PostgreSQL. SQLite serializes writers, so a pool
	// larger than one trades "database is locked" errors for throughput that
	// small deployments do not need — see [DefaultMaxOpenConns].
	MaxOpenConns int `yaml:"maxOpenConns"`

	// MaxIdleConns caps idle connections. Zero means the engine default.
	MaxIdleConns int `yaml:"maxIdleConns"`

	// ConnMaxLifetime recycles connections after this long. Bounded lifetimes
	// keep a connection pool from pinning itself to a failed replica after a
	// failover.
	ConnMaxLifetime Duration `yaml:"connMaxLifetime"`

	// ConnMaxIdleTime closes connections idle for this long.
	ConnMaxIdleTime Duration `yaml:"connMaxIdleTime"`

	// AutoMigrate runs pending migrations on startup. Convenient for the
	// single-binary install, and deliberately off for anything else: a
	// clustered rollout wants migrations run once, deliberately, not raced by
	// every replica as it boots.
	AutoMigrate bool `yaml:"autoMigrate"`
}

// ServerConfig controls the HTTP listener.
type ServerConfig struct {
	// Host to bind. Empty binds all interfaces.
	Host string `yaml:"host"`

	// Port to listen on.
	Port int `yaml:"port"`

	// ReadHeaderTimeout bounds how long a client may take to send headers.
	// This is the defense against Slowloris, so it is deliberately short and
	// separate from ReadTimeout.
	ReadHeaderTimeout Duration `yaml:"readHeaderTimeout"`

	// ReadTimeout bounds reading the entire request, body included.
	ReadTimeout Duration `yaml:"readTimeout"`

	// WriteTimeout bounds writing the response. Query results can be large and
	// slow, so this is generous; streaming endpoints manage their own deadlines.
	WriteTimeout Duration `yaml:"writeTimeout"`

	// IdleTimeout bounds how long a keep-alive connection may sit unused.
	IdleTimeout Duration `yaml:"idleTimeout"`

	// ShutdownTimeout bounds draining in-flight requests on SIGTERM before the
	// process exits anyway.
	ShutdownTimeout Duration `yaml:"shutdownTimeout"`

	// BaseURL is the externally reachable root, like https://pivot.example.
	//
	// It matters for exactly one thing today: the OpenID Connect redirect URI,
	// which must match what is registered at the identity provider byte for
	// byte. Empty derives it from each request's scheme and Host header, which
	// is right for a local install and wrong behind a proxy that rewrites
	// either — so a real deployment sets it.
	BaseURL string `yaml:"baseURL"`

	// PreShutdownDelay is the "lame duck" period: on SIGTERM the server fails
	// its readiness probe, keeps serving for this long, and only then stops
	// accepting connections.
	//
	// Without it, failing readiness first accomplishes nothing — http.Shutdown
	// stops accepting immediately, so a load balancer polling /readyz gets a
	// connection refusal rather than a 503, and keeps routing to an instance
	// that is already gone.
	//
	// Zero by default so local Ctrl-C is instant. Behind a load balancer set
	// it above the readiness probe interval (5s suits Kubernetes defaults).
	PreShutdownDelay Duration `yaml:"preShutdownDelay"`
}

// AuthConfig controls sessions and the session cookie.
type AuthConfig struct {
	// SessionIdleTimeout ends a session that has not been used. It moves
	// forward on use.
	SessionIdleTimeout Duration `yaml:"sessionIdleTimeout"`

	// SessionAbsoluteTimeout is a hard cap that is never extended. Without it
	// a stolen token stays valid indefinitely as long as the thief keeps
	// using it.
	SessionAbsoluteTimeout Duration `yaml:"sessionAbsoluteTimeout"`

	// MaxFailedAttempts before an address is locked out.
	MaxFailedAttempts int `yaml:"maxFailedAttempts"`

	// LockoutDuration is the base lockout window; it doubles with each further
	// lockout up to LockoutMaxDuration.
	LockoutDuration    Duration `yaml:"lockoutDuration"`
	LockoutMaxDuration Duration `yaml:"lockoutMaxDuration"`

	// CookieName is the session cookie. Changing it invalidates every browser
	// session, since the old cookie is simply no longer read.
	CookieName string `yaml:"cookieName"`

	// CookieDomain scopes the cookie. Empty means host-only, which is both the
	// safest default and what a single-host install wants.
	CookieDomain string `yaml:"cookieDomain"`

	// CookieSecure forces the Secure attribute even on a plaintext request.
	//
	// It is off by default so that `pivot serve` on http://localhost works
	// without configuration — a Secure cookie is simply discarded by the
	// browser over plain HTTP, which would make the 30-second install end at a
	// login page that never logs anyone in. A TLS request gets Secure
	// regardless. Behind a proxy that terminates TLS, Pivot sees plain HTTP
	// and cannot tell, so **set this to true in that deployment**.
	CookieSecure bool `yaml:"cookieSecure"`
}

// LogConfig controls structured logging.
type LogConfig struct {
	// Level is one of debug, info, warn, error.
	Level string `yaml:"level"`

	// Format is json or text. JSON is the default because logs are read by
	// machines far more often than by people.
	Format string `yaml:"format"`

	// AddSource attaches the calling file and line. Useful when debugging,
	// costly in hot paths, so it is off by default.
	AddSource bool `yaml:"addSource"`
}

// Address returns the host:port the server binds.
func (s ServerConfig) Address() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

// Default returns the configuration used when nothing else is supplied.
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Host:              "",
			Port:              8080,
			ReadHeaderTimeout: Duration(5 * time.Second),
			ReadTimeout:       Duration(30 * time.Second),
			WriteTimeout:      Duration(120 * time.Second),
			IdleTimeout:       Duration(90 * time.Second),
			ShutdownTimeout:   Duration(30 * time.Second),
			BaseURL:           "",
			PreShutdownDelay:  0,
		},
		Database: DatabaseConfig{
			URL:             "pivot.db",
			MaxOpenConns:    0, // engine default
			MaxIdleConns:    0, // engine default
			ConnMaxLifetime: Duration(time.Hour),
			ConnMaxIdleTime: Duration(10 * time.Minute),
			AutoMigrate:     true,
		},
		Auth: AuthConfig{
			SessionIdleTimeout:     Duration(8 * time.Hour),
			SessionAbsoluteTimeout: Duration(30 * 24 * time.Hour),
			MaxFailedAttempts:      10,
			LockoutDuration:        Duration(time.Minute),
			LockoutMaxDuration:     Duration(time.Hour),
			CookieName:             "pivot_session",
			CookieDomain:           "",
			CookieSecure:           false,
		},
		Log: LogConfig{
			Level:     "info",
			Format:    "json",
			AddSource: false,
		},
		Observability: ObservabilityConfig{
			Tracing: TracingConfig{
				Enabled:     false,
				Endpoint:    "localhost:4318",
				Insecure:    true,
				SampleRatio: 1.0,
				ServiceName: "pivot",
			},
			Metrics: MetricsConfig{Enabled: true},
		},

		// Empty, so a token is generated per process. See SetupConfig.Token.
		Setup: SetupConfig{Token: ""},

		// Empty key and key file: a key is generated on first run and written
		// beside the database. See SecretsConfig.KeyFile.
		Secrets: SecretsConfig{},
	}
}

// Duration wraps [time.Duration] so configuration files can say "30s" instead
// of a nanosecond count. The zero value is a zero duration.
type Duration time.Duration

// Duration returns the underlying [time.Duration].
func (d Duration) Duration() time.Duration { return time.Duration(d) }

// String implements [fmt.Stringer].
func (d Duration) String() string { return time.Duration(d).String() }

// UnmarshalYAML accepts either a duration string ("30s") or a plain integer
// count of seconds, because both are things people reasonably write.
//
// The YAML tag is what distinguishes them. Decoding into a string first does
// not work: yaml.v3 will happily render the scalar 120 as "120", which then
// fails to parse as a duration.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Tag == "!!int" {
		var secs int64
		if err := node.Decode(&secs); err != nil {
			return fmt.Errorf("parse duration %q: %w", node.Value, err)
		}
		*d = Duration(time.Duration(secs) * time.Second)

		return nil
	}

	var s string
	if err := node.Decode(&s); err != nil {
		return fmt.Errorf(
			"duration must be a string like %q or a number of seconds, got %q",
			"30s", node.Value,
		)
	}

	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", s, err)
	}
	*d = Duration(parsed)

	return nil
}

// MarshalYAML writes durations back in their human-readable form.
func (d Duration) MarshalYAML() (any, error) { return d.String(), nil }
