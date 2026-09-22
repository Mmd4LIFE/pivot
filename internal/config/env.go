package config

import (
	"fmt"
	"strconv"
	"time"
)

// EnvPrefix is the prefix for every Pivot environment variable.
const EnvPrefix = "PIVOT_"

// binding maps one environment variable to one configuration field.
//
// The table is explicit rather than reflection-driven on purpose: it is the
// canonical list of supported variables, `pivot config show` prints it, and a
// typo in a key becomes a compile-time concern instead of a silent no-op.
type binding struct {
	key   string
	help  string
	apply func(*Config, string) error
}

// bindings is the complete set of environment variables Pivot reads. Adding a
// field to [Config] means adding it here too.
func bindings() []binding {
	return []binding{
		{
			key:  EnvPrefix + "SERVER_HOST",
			help: "Interface to bind; empty binds all",
			apply: func(c *Config, v string) error {
				c.Server.Host = v

				return nil
			},
		},
		{
			key:  EnvPrefix + "SERVER_PORT",
			help: "Port to listen on",
			apply: func(c *Config, v string) error {
				return setInt(&c.Server.Port, v)
			},
		},
		{
			key:  EnvPrefix + "SERVER_READ_HEADER_TIMEOUT",
			help: "Max time to receive request headers",
			apply: func(c *Config, v string) error {
				return setDuration(&c.Server.ReadHeaderTimeout, v)
			},
		},
		{
			key:  EnvPrefix + "SERVER_READ_TIMEOUT",
			help: "Max time to read the whole request",
			apply: func(c *Config, v string) error {
				return setDuration(&c.Server.ReadTimeout, v)
			},
		},
		{
			key:  EnvPrefix + "SERVER_WRITE_TIMEOUT",
			help: "Max time to write the response",
			apply: func(c *Config, v string) error {
				return setDuration(&c.Server.WriteTimeout, v)
			},
		},
		{
			key:  EnvPrefix + "SERVER_IDLE_TIMEOUT",
			help: "Max idle time on a keep-alive connection",
			apply: func(c *Config, v string) error {
				return setDuration(&c.Server.IdleTimeout, v)
			},
		},
		{
			key:  EnvPrefix + "SERVER_SHUTDOWN_TIMEOUT",
			help: "Max time to drain in-flight requests on shutdown",
			apply: func(c *Config, v string) error {
				return setDuration(&c.Server.ShutdownTimeout, v)
			},
		},
		{
			key:  EnvPrefix + "SERVER_PRE_SHUTDOWN_DELAY",
			help: "Lame-duck period: fail readiness, keep serving, then stop accepting",
			apply: func(c *Config, v string) error {
				return setDuration(&c.Server.PreShutdownDelay, v)
			},
		},
		{
			key:  EnvPrefix + "SERVER_BASE_URL",
			help: "Externally reachable root, e.g. https://pivot.example; required behind a proxy",
			apply: func(c *Config, v string) error {
				c.Server.BaseURL = v

				return nil
			},
		},
		{
			key:  EnvPrefix + "DATABASE_URL",
			help: "Metadata database: a SQLite path, sqlite://, :memory:, or postgres://",
			apply: func(c *Config, v string) error {
				c.Database.URL = v

				return nil
			},
		},
		{
			key:  EnvPrefix + "DATABASE_MAX_OPEN_CONNS",
			help: "Max concurrent connections; 0 uses the engine default",
			apply: func(c *Config, v string) error {
				return setInt(&c.Database.MaxOpenConns, v)
			},
		},
		{
			key:  EnvPrefix + "DATABASE_MAX_IDLE_CONNS",
			help: "Max idle connections; 0 uses the engine default",
			apply: func(c *Config, v string) error {
				return setInt(&c.Database.MaxIdleConns, v)
			},
		},
		{
			key:  EnvPrefix + "DATABASE_CONN_MAX_LIFETIME",
			help: "Recycle connections after this long",
			apply: func(c *Config, v string) error {
				return setDuration(&c.Database.ConnMaxLifetime, v)
			},
		},
		{
			key:  EnvPrefix + "DATABASE_CONN_MAX_IDLE_TIME",
			help: "Close connections idle for this long",
			apply: func(c *Config, v string) error {
				return setDuration(&c.Database.ConnMaxIdleTime, v)
			},
		},
		{
			key:  EnvPrefix + "DATABASE_AUTO_MIGRATE",
			help: "Run pending migrations on startup",
			apply: func(c *Config, v string) error {
				return setBool(&c.Database.AutoMigrate, v)
			},
		},
		{
			key:  EnvPrefix + "AUTH_SESSION_IDLE_TIMEOUT",
			help: "End a session unused for this long; slides forward on use",
			apply: func(c *Config, v string) error {
				return setDuration(&c.Auth.SessionIdleTimeout, v)
			},
		},
		{
			key:  EnvPrefix + "AUTH_SESSION_ABSOLUTE_TIMEOUT",
			help: "Hard session cap, never extended",
			apply: func(c *Config, v string) error {
				return setDuration(&c.Auth.SessionAbsoluteTimeout, v)
			},
		},
		{
			key:  EnvPrefix + "AUTH_MAX_FAILED_ATTEMPTS",
			help: "Failed logins before an address is locked out",
			apply: func(c *Config, v string) error {
				return setInt(&c.Auth.MaxFailedAttempts, v)
			},
		},
		{
			key:  EnvPrefix + "AUTH_LOCKOUT_DURATION",
			help: "Base lockout window; doubles with each further lockout",
			apply: func(c *Config, v string) error {
				return setDuration(&c.Auth.LockoutDuration, v)
			},
		},
		{
			key:  EnvPrefix + "AUTH_LOCKOUT_MAX_DURATION",
			help: "Cap on the doubling lockout window",
			apply: func(c *Config, v string) error {
				return setDuration(&c.Auth.LockoutMaxDuration, v)
			},
		},
		{
			key:  EnvPrefix + "AUTH_COOKIE_NAME",
			help: "Session cookie name; changing it ends every browser session",
			apply: func(c *Config, v string) error {
				c.Auth.CookieName = v

				return nil
			},
		},
		{
			key:  EnvPrefix + "AUTH_COOKIE_DOMAIN",
			help: "Cookie domain; empty is host-only",
			apply: func(c *Config, v string) error {
				c.Auth.CookieDomain = v

				return nil
			},
		},
		{
			key:  EnvPrefix + "AUTH_COOKIE_SECURE",
			help: "Force the Secure cookie attribute; set this behind a TLS proxy",
			apply: func(c *Config, v string) error {
				return setBool(&c.Auth.CookieSecure, v)
			},
		},
		{
			key:  EnvPrefix + "LOG_LEVEL",
			help: "debug, info, warn, or error",
			apply: func(c *Config, v string) error {
				c.Log.Level = v

				return nil
			},
		},
		{
			key:  EnvPrefix + "LOG_FORMAT",
			help: "json or text",
			apply: func(c *Config, v string) error {
				c.Log.Format = v

				return nil
			},
		},
		{
			key:  EnvPrefix + "TRACING_ENABLED",
			help: "Export traces over OTLP",
			apply: func(c *Config, v string) error {
				return setBool(&c.Observability.Tracing.Enabled, v)
			},
		},
		{
			key:  EnvPrefix + "TRACING_ENDPOINT",
			help: "Collector address for OTLP/HTTP, host:port with no scheme",
			apply: func(c *Config, v string) error {
				c.Observability.Tracing.Endpoint = v

				return nil
			},
		},
		{
			key:  EnvPrefix + "TRACING_INSECURE",
			help: "Send traces over plain HTTP; only for a collector alongside Pivot",
			apply: func(c *Config, v string) error {
				return setBool(&c.Observability.Tracing.Insecure, v)
			},
		},
		{
			key:  EnvPrefix + "TRACING_SAMPLE_RATIO",
			help: "Fraction of traces to keep, 0 to 1",
			apply: func(c *Config, v string) error {
				return setFloat(&c.Observability.Tracing.SampleRatio, v)
			},
		},
		{
			key:  EnvPrefix + "TRACING_SERVICE_NAME",
			help: "Name this process reports to the collector",
			apply: func(c *Config, v string) error {
				c.Observability.Tracing.ServiceName = v

				return nil
			},
		},
		{
			key:  EnvPrefix + "LOG_ADD_SOURCE",
			help: "Attach caller file and line to log records",
			apply: func(c *Config, v string) error {
				return setBool(&c.Log.AddSource, v)
			},
		},
	}
}

// EnvVars returns the supported environment variables and their descriptions,
// in declaration order, for help output.
func EnvVars() []struct{ Key, Help string } {
	b := bindings()
	out := make([]struct{ Key, Help string }, 0, len(b))

	for _, bind := range b {
		out = append(out, struct{ Key, Help string }{bind.key, bind.help})
	}

	return out
}

// applyEnv overlays environment variables onto c.
//
// An unset variable leaves the field alone; a set-but-empty variable is
// honored, because PIVOT_SERVER_HOST="" is a meaningful way to say "bind all
// interfaces".
func applyEnv(c *Config, lookup func(string) (string, bool)) error {
	for _, bind := range bindings() {
		raw, ok := lookup(bind.key)
		if !ok {
			continue
		}

		if err := bind.apply(c, raw); err != nil {
			return fmt.Errorf("%s: %w", bind.key, err)
		}
	}

	return nil
}

func setInt(dst *int, raw string) error {
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("expected an integer, got %q", raw)
	}
	*dst = v

	return nil
}

func setBool(dst *bool, raw string) error {
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fmt.Errorf("expected a boolean (true/false/1/0), got %q", raw)
	}
	*dst = v

	return nil
}

func setFloat(dst *float64, raw string) error {
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fmt.Errorf("expected a number, got %q", raw)
	}
	*dst = v

	return nil
}

func setDuration(dst *Duration, raw string) error {
	v, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("expected a duration like \"30s\", got %q", raw)
	}
	*dst = Duration(v)

	return nil
}
