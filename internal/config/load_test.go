package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/config"
)

// envMap builds a lookup function over a fixed map, so tests never touch the
// real process environment.
func envMap(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]

		return v, ok
	}
}

// writeConfig writes a config file into a temp dir and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "pivot.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

func TestLoadDefaults(t *testing.T) {
	t.Parallel()

	res, err := config.Load(config.Options{Lookup: envMap(nil)})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	got := res.Config
	if got.Server.Port != 8080 {
		t.Errorf("Server.Port = %d, want 8080", got.Server.Port)
	}

	if got.Log.Level != "info" {
		t.Errorf("Log.Level = %q, want %q", got.Log.Level, "info")
	}

	if got.Log.Format != "json" {
		t.Errorf("Log.Format = %q, want %q", got.Log.Format, "json")
	}

	if res.SourceFile != "" {
		t.Errorf("SourceFile = %q, want empty when no file is found", res.SourceFile)
	}
}

// The precedence chain is the whole point of this package, so it is asserted
// layer by layer and then all at once.
func TestLoadPrecedence(t *testing.T) {
	t.Parallel()

	const file = `
server:
  port: 1111
log:
  level: warn
  format: text
`

	t.Run("file beats defaults", func(t *testing.T) {
		t.Parallel()

		res, err := config.Load(config.Options{
			Path:   writeConfig(t, file),
			Lookup: envMap(nil),
		})
		if err != nil {
			t.Fatalf("Load: %v", err)
		}

		if res.Config.Server.Port != 1111 {
			t.Errorf("Server.Port = %d, want 1111 from file", res.Config.Server.Port)
		}

		if res.Config.Log.Level != "warn" {
			t.Errorf("Log.Level = %q, want %q from file", res.Config.Log.Level, "warn")
		}
	})

	t.Run("env beats file", func(t *testing.T) {
		t.Parallel()

		res, err := config.Load(config.Options{
			Path: writeConfig(t, file),
			Lookup: envMap(map[string]string{
				"PIVOT_SERVER_PORT": "2222",
				"PIVOT_LOG_LEVEL":   "debug",
			}),
		})
		if err != nil {
			t.Fatalf("Load: %v", err)
		}

		if res.Config.Server.Port != 2222 {
			t.Errorf("Server.Port = %d, want 2222 from env", res.Config.Server.Port)
		}

		if res.Config.Log.Level != "debug" {
			t.Errorf("Log.Level = %q, want %q from env", res.Config.Log.Level, "debug")
		}

		// Untouched by env, so the file value must survive.
		if res.Config.Log.Format != "text" {
			t.Errorf("Log.Format = %q, want %q still from file", res.Config.Log.Format, "text")
		}
	})

	t.Run("flags beat env", func(t *testing.T) {
		t.Parallel()

		res, err := config.Load(config.Options{
			Path: writeConfig(t, file),
			Lookup: envMap(map[string]string{
				"PIVOT_SERVER_PORT": "2222",
				"PIVOT_LOG_LEVEL":   "debug",
			}),
			Override: func(c *config.Config) {
				c.Server.Port = 3333
			},
		})
		if err != nil {
			t.Fatalf("Load: %v", err)
		}

		if res.Config.Server.Port != 3333 {
			t.Errorf("Server.Port = %d, want 3333 from flag", res.Config.Server.Port)
		}

		// The flag touched only the port, so env must still win for the level.
		if res.Config.Log.Level != "debug" {
			t.Errorf("Log.Level = %q, want %q still from env", res.Config.Log.Level, "debug")
		}
	})
}

// A layer must overwrite only what it specifies. Setting the port in a file
// must not silently reset the log level to its default.
func TestLoadLayersArePartial(t *testing.T) {
	t.Parallel()

	res, err := config.Load(config.Options{
		Path:   writeConfig(t, "server:\n  port: 1234\n"),
		Lookup: envMap(nil),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if res.Config.Server.Port != 1234 {
		t.Errorf("Server.Port = %d, want 1234", res.Config.Server.Port)
	}

	if res.Config.Log.Level != "info" {
		t.Errorf("Log.Level = %q, want the default %q", res.Config.Log.Level, "info")
	}

	if got := res.Config.Server.ReadTimeout.Duration(); got != 30*time.Second {
		t.Errorf("Server.ReadTimeout = %v, want the default 30s", got)
	}
}

func TestLoadDurationsFromFile(t *testing.T) {
	t.Parallel()

	res, err := config.Load(config.Options{
		Path:   writeConfig(t, "server:\n  readTimeout: 45s\n  idleTimeout: 120\n"),
		Lookup: envMap(nil),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := res.Config.Server.ReadTimeout.Duration(); got != 45*time.Second {
		t.Errorf("ReadTimeout = %v, want 45s", got)
	}

	// A bare number is accepted as seconds.
	if got := res.Config.Server.IdleTimeout.Duration(); got != 120*time.Second {
		t.Errorf("IdleTimeout = %v, want 120s", got)
	}
}

func TestLoadDurationFromEnv(t *testing.T) {
	t.Parallel()

	res, err := config.Load(config.Options{
		Lookup: envMap(map[string]string{"PIVOT_SERVER_SHUTDOWN_TIMEOUT": "5s"}),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := res.Config.Server.ShutdownTimeout.Duration(); got != 5*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 5s", got)
	}
}

// A set-but-empty variable is meaningful: it is how you say "bind all
// interfaces" after a config file set a specific host.
func TestLoadEmptyEnvValueIsHonored(t *testing.T) {
	t.Parallel()

	res, err := config.Load(config.Options{
		Path:   writeConfig(t, "server:\n  host: 127.0.0.1\n"),
		Lookup: envMap(map[string]string{"PIVOT_SERVER_HOST": ""}),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if res.Config.Server.Host != "" {
		t.Errorf("Server.Host = %q, want empty from the env override", res.Config.Server.Host)
	}
}

func TestLoadMissingExplicitFileIsAnError(t *testing.T) {
	t.Parallel()

	_, err := config.Load(config.Options{
		Path:   filepath.Join(t.TempDir(), "absent.yaml"),
		Lookup: envMap(nil),
	})
	if err == nil {
		t.Fatal("Load returned nil error for a missing explicit config file")
	}
}

// A typo that silently does nothing is worse than a startup error.
func TestLoadRejectsUnknownKeys(t *testing.T) {
	t.Parallel()

	_, err := config.Load(config.Options{
		Path:   writeConfig(t, "server:\n  prot: 9000\n"),
		Lookup: envMap(nil),
	})
	if err == nil {
		t.Fatal("Load accepted an unknown key; want an error naming it")
	}
}

func TestLoadRejectsBadEnvValue(t *testing.T) {
	t.Parallel()

	_, err := config.Load(config.Options{
		Lookup: envMap(map[string]string{"PIVOT_SERVER_PORT": "not-a-number"}),
	})
	if err == nil {
		t.Fatal("Load accepted a non-numeric port")
	}
}

func TestLoadRunsValidation(t *testing.T) {
	t.Parallel()

	_, err := config.Load(config.Options{
		Lookup: envMap(map[string]string{"PIVOT_SERVER_PORT": "70000"}),
	})
	if err == nil {
		t.Fatal("Load accepted an out-of-range port")
	}
}

func TestLoadEmptyFileIsFine(t *testing.T) {
	t.Parallel()

	res, err := config.Load(config.Options{
		Path:   writeConfig(t, ""),
		Lookup: envMap(nil),
	})
	if err != nil {
		t.Fatalf("Load rejected an empty config file: %v", err)
	}

	if res.Config.Server.Port != 8080 {
		t.Errorf("Server.Port = %d, want the default 8080", res.Config.Server.Port)
	}
}
