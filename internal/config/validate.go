package config

import (
	"fmt"
	"slices"
	"strings"
)

// Valid values, exported so the CLI can list them in help text rather than
// duplicating the strings and drifting.
var (
	LogLevels  = []string{"debug", "info", "warn", "error"}
	LogFormats = []string{"json", "text"}
)

const maxPort = 65535

// FieldError is a single invalid configuration value.
type FieldError struct {
	// Field is the YAML path, e.g. "server.port".
	Field string

	// Value is what was supplied.
	Value any

	// Want describes what would have been acceptable.
	Want string
}

func (e FieldError) Error() string {
	return fmt.Sprintf("%s: invalid value %v (want %s)", e.Field, e.Value, e.Want)
}

// ValidationError collects every problem found in one pass, so a user fixing
// their config sees all of it at once instead of one error per restart.
type ValidationError struct {
	Errors []FieldError
}

func (e *ValidationError) Error() string {
	if len(e.Errors) == 1 {
		return "invalid configuration: " + e.Errors[0].Error()
	}

	var b strings.Builder
	fmt.Fprintf(&b, "invalid configuration (%d problems):", len(e.Errors))

	for _, fe := range e.Errors {
		b.WriteString("\n  - ")
		b.WriteString(fe.Error())
	}

	return b.String()
}

// Validate checks every field and returns a [*ValidationError] listing all
// problems, or nil when the configuration is usable.
func (c *Config) Validate() error {
	var errs []FieldError

	if c.Server.Port < 1 || c.Server.Port > maxPort {
		errs = append(errs, FieldError{
			Field: "server.port",
			Value: c.Server.Port,
			Want:  fmt.Sprintf("1-%d", maxPort),
		})
	}

	durations := []struct {
		field string
		value Duration
	}{
		{"server.readHeaderTimeout", c.Server.ReadHeaderTimeout},
		{"server.readTimeout", c.Server.ReadTimeout},
		{"server.writeTimeout", c.Server.WriteTimeout},
		{"server.idleTimeout", c.Server.IdleTimeout},
		{"server.shutdownTimeout", c.Server.ShutdownTimeout},
		{"auth.sessionIdleTimeout", c.Auth.SessionIdleTimeout},
		{"auth.sessionAbsoluteTimeout", c.Auth.SessionAbsoluteTimeout},
		{"auth.lockoutDuration", c.Auth.LockoutDuration},
		{"auth.lockoutMaxDuration", c.Auth.LockoutMaxDuration},
	}

	for _, d := range durations {
		if d.value <= 0 {
			errs = append(errs, FieldError{
				Field: d.field,
				Value: d.value.String(),
				Want:  "a positive duration such as 30s",
			})
		}
	}

	// Zero is valid here: it means "no lame-duck period", which is the right
	// default outside a load balancer.
	if c.Server.PreShutdownDelay < 0 {
		errs = append(errs, FieldError{
			Field: "server.preShutdownDelay",
			Value: c.Server.PreShutdownDelay.String(),
			Want:  "a non-negative duration such as 5s, or 0 to disable",
		})
	}

	if strings.TrimSpace(c.Database.URL) == "" {
		errs = append(errs, FieldError{
			Field: "database.url",
			Value: `""`,
			Want:  "a SQLite path, sqlite://, :memory:, or postgres:// URL",
		})
	}

	if c.Database.MaxOpenConns < 0 {
		errs = append(errs, FieldError{
			Field: "database.maxOpenConns",
			Value: c.Database.MaxOpenConns,
			Want:  "a non-negative count, or 0 for the engine default",
		})
	}

	if c.Database.MaxIdleConns < 0 {
		errs = append(errs, FieldError{
			Field: "database.maxIdleConns",
			Value: c.Database.MaxIdleConns,
			Want:  "a non-negative count, or 0 for the engine default",
		})
	}

	// An idle timeout beyond the absolute cap is not an error the server would
	// ever notice — the cap simply wins — but it means the operator believes
	// sessions last longer than they do, which is worth saying out loud.
	if c.Auth.SessionIdleTimeout > 0 && c.Auth.SessionIdleTimeout > c.Auth.SessionAbsoluteTimeout {
		errs = append(errs, FieldError{
			Field: "auth.sessionIdleTimeout",
			Value: c.Auth.SessionIdleTimeout.String(),
			Want:  "no longer than auth.sessionAbsoluteTimeout (" + c.Auth.SessionAbsoluteTimeout.String() + ")",
		})
	}

	if c.Auth.LockoutDuration > 0 && c.Auth.LockoutDuration > c.Auth.LockoutMaxDuration {
		errs = append(errs, FieldError{
			Field: "auth.lockoutDuration",
			Value: c.Auth.LockoutDuration.String(),
			Want:  "no longer than auth.lockoutMaxDuration (" + c.Auth.LockoutMaxDuration.String() + ")",
		})
	}

	if c.Auth.MaxFailedAttempts < 1 {
		errs = append(errs, FieldError{
			Field: "auth.maxFailedAttempts",
			Value: c.Auth.MaxFailedAttempts,
			Want:  "at least 1; there is no way to disable lockout",
		})
	}

	if strings.TrimSpace(c.Auth.CookieName) == "" {
		errs = append(errs, FieldError{
			Field: "auth.cookieName",
			Value: `""`,
			Want:  "a cookie name such as pivot_session",
		})
	}

	if !slices.Contains(LogLevels, c.Log.Level) {
		errs = append(errs, FieldError{
			Field: "log.level",
			Value: c.Log.Level,
			Want:  "one of " + strings.Join(LogLevels, ", "),
		})
	}

	if !slices.Contains(LogFormats, c.Log.Format) {
		errs = append(errs, FieldError{
			Field: "log.format",
			Value: c.Log.Format,
			Want:  "one of " + strings.Join(LogFormats, ", "),
		})
	}

	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}

	return nil
}
