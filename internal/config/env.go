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

func setDuration(dst *Duration, raw string) error {
	v, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("expected a duration like \"30s\", got %q", raw)
	}
	*dst = Duration(v)

	return nil
}
