package config_test

import (
	"strings"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/config"
)

/*
The environment table, exercised entry by entry.

`bindings()` is a list of every PIVOT_* variable Pivot reads, and each entry
carries a closure that writes one field. Nothing had ever called most of those
closures: the table was 33% covered, which meant a binding could point at the
wrong field, or fail to parse a value, and no test would notice. That is a bad
place for a silent bug -- an operator sets a variable, the server reports
success, and the setting does nothing.

So this walks the whole table: every variable is set to a value, and the
resolved configuration is asked whether it took.
*/

// The value to set for each variable, chosen to be valid for its field.
var sample = map[string]string{
	"PIVOT_SERVER_HOST":                   "127.0.0.1",
	"PIVOT_SERVER_PORT":                   "9090",
	"PIVOT_SERVER_READ_HEADER_TIMEOUT":    "3s",
	"PIVOT_SERVER_READ_TIMEOUT":           "20s",
	"PIVOT_SERVER_WRITE_TIMEOUT":          "25s",
	"PIVOT_SERVER_IDLE_TIMEOUT":           "90s",
	"PIVOT_SERVER_SHUTDOWN_TIMEOUT":       "15s",
	"PIVOT_SERVER_PRE_SHUTDOWN_DELAY":     "2s",
	"PIVOT_SERVER_BASE_URL":               "https://pivot.example",
	"PIVOT_DATABASE_URL":                  "sqlite://test.db",
	"PIVOT_DATABASE_MAX_OPEN_CONNS":       "17",
	"PIVOT_DATABASE_MAX_IDLE_CONNS":       "5",
	"PIVOT_DATABASE_CONN_MAX_LIFETIME":    "45m",
	"PIVOT_DATABASE_CONN_MAX_IDLE_TIME":   "7m",
	"PIVOT_DATABASE_AUTO_MIGRATE":         "false",
	"PIVOT_AUTH_SESSION_IDLE_TIMEOUT":     "2h",
	"PIVOT_AUTH_SESSION_ABSOLUTE_TIMEOUT": "48h",
	"PIVOT_AUTH_MAX_FAILED_ATTEMPTS":      "9",
	"PIVOT_AUTH_LOCKOUT_DURATION":         "3m",
	"PIVOT_AUTH_LOCKOUT_MAX_DURATION":     "30m",
	"PIVOT_AUTH_COOKIE_NAME":              "pivot_test_session",
	"PIVOT_AUTH_COOKIE_DOMAIN":            "pivot.example",
	"PIVOT_AUTH_COOKIE_SECURE":            "true",
	"PIVOT_LOG_LEVEL":                     "debug",
	"PIVOT_LOG_FORMAT":                    "text",
	"PIVOT_LOG_ADD_SOURCE":                "true",
	"PIVOT_METRICS_ENABLED":               "false",
	"PIVOT_SETUP_TOKEN":                   "a-token-from-the-orchestrator",
	"PIVOT_SECRETS_KEY":                   "c2l4dGVlbi1ieXRlcy10aW1lcy10d28tZXhhY3RseSE=",
	"PIVOT_SECRETS_KEY_FILE":              "/run/secrets/pivot.key",
	"PIVOT_SECRETS_PREVIOUS_KEYS":         "b25lLWtleQ==,YW5vdGhlci1rZXk=",
	"PIVOT_TRACING_ENABLED":               "true",
	"PIVOT_TRACING_ENDPOINT":              "collector.internal:4318",
	"PIVOT_TRACING_INSECURE":              "false",
	"PIVOT_TRACING_SAMPLE_RATIO":          "0.25",
	"PIVOT_TRACING_SERVICE_NAME":          "pivot-test",
}

func TestEveryEnvironmentVariableIsBound(t *testing.T) {
	t.Parallel()

	// Every variable Pivot documents must have a sample above, or this test is
	// quietly skipping the one that was just added.
	for _, v := range config.EnvVars() {
		if _, ok := sample[v.Key]; !ok {
			t.Errorf("%s is documented but this test has no sample value for it", v.Key)
		}
	}

	for key := range sample {
		known := false

		for _, v := range config.EnvVars() {
			if v.Key == key {
				known = true
			}
		}

		if !known {
			t.Errorf("%s is sampled here but Pivot does not read it", key)
		}
	}
}

func TestEveryEnvironmentVariableTakesEffect(t *testing.T) {
	t.Parallel()

	res, err := config.Load(config.Options{
		Lookup: func(k string) (string, bool) {
			v, ok := sample[k]

			return v, ok
		},
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	c := res.Config

	checks := []struct {
		key  string
		got  any
		want any
	}{
		{"PIVOT_SERVER_HOST", c.Server.Host, "127.0.0.1"},
		{"PIVOT_SERVER_PORT", c.Server.Port, 9090},
		{"PIVOT_SERVER_BASE_URL", c.Server.BaseURL, "https://pivot.example"},
		{"PIVOT_DATABASE_URL", c.Database.URL, "sqlite://test.db"},
		{"PIVOT_DATABASE_MAX_OPEN_CONNS", c.Database.MaxOpenConns, 17},
		{"PIVOT_DATABASE_MAX_IDLE_CONNS", c.Database.MaxIdleConns, 5},
		{"PIVOT_DATABASE_AUTO_MIGRATE", c.Database.AutoMigrate, false},
		{"PIVOT_AUTH_MAX_FAILED_ATTEMPTS", c.Auth.MaxFailedAttempts, 9},
		{"PIVOT_AUTH_COOKIE_NAME", c.Auth.CookieName, "pivot_test_session"},
		{"PIVOT_AUTH_COOKIE_DOMAIN", c.Auth.CookieDomain, "pivot.example"},
		{"PIVOT_AUTH_COOKIE_SECURE", c.Auth.CookieSecure, true},
		{"PIVOT_LOG_LEVEL", c.Log.Level, "debug"},
		{"PIVOT_LOG_FORMAT", c.Log.Format, "text"},
		{"PIVOT_LOG_ADD_SOURCE", c.Log.AddSource, true},
		{"PIVOT_METRICS_ENABLED", c.Observability.Metrics.Enabled, false},
		{"PIVOT_SETUP_TOKEN", c.Setup.Token, "a-token-from-the-orchestrator"},
		{"PIVOT_SECRETS_KEY", c.Secrets.Key, "c2l4dGVlbi1ieXRlcy10aW1lcy10d28tZXhhY3RseSE="},
		{"PIVOT_SECRETS_KEY_FILE", c.Secrets.KeyFile, "/run/secrets/pivot.key"},
		{"PIVOT_TRACING_ENABLED", c.Observability.Tracing.Enabled, true},
		{"PIVOT_TRACING_ENDPOINT", c.Observability.Tracing.Endpoint, "collector.internal:4318"},
		{"PIVOT_TRACING_INSECURE", c.Observability.Tracing.Insecure, false},
		{"PIVOT_TRACING_SAMPLE_RATIO", c.Observability.Tracing.SampleRatio, 0.25},
		{"PIVOT_TRACING_SERVICE_NAME", c.Observability.Tracing.ServiceName, "pivot-test"},
	}

	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("%s: got %v, want %v", check.key, check.got, check.want)
		}
	}

	// The durations, which do not compare cleanly as `any`.
	durations := []struct {
		key  string
		got  config.Duration
		want string
	}{
		{"PIVOT_SERVER_READ_HEADER_TIMEOUT", c.Server.ReadHeaderTimeout, "3s"},
		{"PIVOT_SERVER_READ_TIMEOUT", c.Server.ReadTimeout, "20s"},
		{"PIVOT_SERVER_WRITE_TIMEOUT", c.Server.WriteTimeout, "25s"},
		{"PIVOT_SERVER_IDLE_TIMEOUT", c.Server.IdleTimeout, "1m30s"},
		{"PIVOT_SERVER_SHUTDOWN_TIMEOUT", c.Server.ShutdownTimeout, "15s"},
		{"PIVOT_SERVER_PRE_SHUTDOWN_DELAY", c.Server.PreShutdownDelay, "2s"},
		{"PIVOT_DATABASE_CONN_MAX_LIFETIME", c.Database.ConnMaxLifetime, "45m0s"},
		{"PIVOT_DATABASE_CONN_MAX_IDLE_TIME", c.Database.ConnMaxIdleTime, "7m0s"},
		{"PIVOT_AUTH_SESSION_IDLE_TIMEOUT", c.Auth.SessionIdleTimeout, "2h0m0s"},
		{"PIVOT_AUTH_SESSION_ABSOLUTE_TIMEOUT", c.Auth.SessionAbsoluteTimeout, "48h0m0s"},
		{"PIVOT_AUTH_LOCKOUT_DURATION", c.Auth.LockoutDuration, "3m0s"},
		{"PIVOT_AUTH_LOCKOUT_MAX_DURATION", c.Auth.LockoutMaxDuration, "30m0s"},
	}

	for _, check := range durations {
		if got := check.got.String(); got != check.want {
			t.Errorf("%s: got %s, want %s", check.key, got, check.want)
		}
	}
}

func TestAMalformedEnvironmentValueIsRejected(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"PIVOT_SERVER_PORT":              "not-a-number",
		"PIVOT_SERVER_READ_TIMEOUT":      "not-a-duration",
		"PIVOT_DATABASE_AUTO_MIGRATE":    "perhaps",
		"PIVOT_AUTH_MAX_FAILED_ATTEMPTS": "several",
		"PIVOT_TRACING_SAMPLE_RATIO":     "most of them",
		"PIVOT_TRACING_ENABLED":          "sometimes",
	}

	for key, value := range cases {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(config.Options{
				Lookup: func(k string) (string, bool) {
					if k == key {
						return value, true
					}

					return "", false
				},
			})
			if err == nil {
				t.Fatalf("%s=%q was accepted", key, value)
			}

			// The message has to name the variable. "invalid syntax" on its own
			// leaves an operator grepping their own environment.
			if !strings.Contains(err.Error(), key) {
				t.Errorf("error = %q, want it to name %s", err, key)
			}
		})
	}
}
