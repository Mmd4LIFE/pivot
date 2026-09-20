package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/config"
)

func TestValidateAcceptsDefaults(t *testing.T) {
	t.Parallel()

	if err := config.Default().Validate(); err != nil {
		t.Fatalf("defaults failed validation: %v", err)
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		mutate    func(*config.Config)
		wantField string
	}{
		"port zero":      {func(c *config.Config) { c.Server.Port = 0 }, "server.port"},
		"port negative":  {func(c *config.Config) { c.Server.Port = -1 }, "server.port"},
		"port too large": {func(c *config.Config) { c.Server.Port = 70000 }, "server.port"},
		"bad log level":  {func(c *config.Config) { c.Log.Level = "verbose" }, "log.level"},
		"bad log format": {func(c *config.Config) { c.Log.Format = "xml" }, "log.format"},
		"zero read":      {func(c *config.Config) { c.Server.ReadTimeout = 0 }, "server.readTimeout"},
		"negative shutdown": {
			func(c *config.Config) { c.Server.ShutdownTimeout = -1 },
			"server.shutdownTimeout",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := config.Default()
			tc.mutate(cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatal("Validate returned nil; want an error")
			}

			var ve *config.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("error is %T, want *config.ValidationError", err)
			}

			if !strings.Contains(err.Error(), tc.wantField) {
				t.Errorf("error %q does not name the field %q", err, tc.wantField)
			}
		})
	}
}

// All problems should surface in one pass, so a user fixing their config sees
// everything at once instead of one error per restart.
func TestValidateReportsEveryProblem(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Server.Port = 0
	cfg.Log.Level = "verbose"
	cfg.Log.Format = "xml"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate returned nil; want an error")
	}

	var ve *config.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error is %T, want *config.ValidationError", err)
	}

	if len(ve.Errors) != 3 {
		t.Errorf("got %d field errors, want 3: %v", len(ve.Errors), ve.Errors)
	}
}

// An unusable error message is a real defect: the operator needs to know what
// was wrong and what would be right.
func TestValidationErrorMessageIsActionable(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Log.Level = "verbose"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate returned nil; want an error")
	}

	msg := err.Error()
	for _, want := range []string{"log.level", "verbose", "debug", "info", "warn", "error"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q is missing %q", msg, want)
		}
	}
}

func TestAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		host string
		port int
		want string
	}{
		{"", 8080, ":8080"},
		{"127.0.0.1", 9000, "127.0.0.1:9000"},
		{"0.0.0.0", 80, "0.0.0.0:80"},
	}

	for _, tc := range tests {
		cfg := config.ServerConfig{Host: tc.host, Port: tc.port}
		if got := cfg.Address(); got != tc.want {
			t.Errorf("Address() with host=%q port=%d = %q, want %q", tc.host, tc.port, got, tc.want)
		}
	}
}
