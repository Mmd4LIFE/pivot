package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/cli"
)

// run executes the command tree with the given args and a fixed environment,
// returning stdout, stderr, and the error.
func run(t *testing.T, envVars map[string]string, args ...string) (string, string, error) {
	t.Helper()

	var stdout, stderr bytes.Buffer

	cmd := cli.NewRootCmd(cli.Env{
		Stdout: &stdout,
		Stderr: &stderr,
		Lookup: func(k string) (string, bool) {
			v, ok := envVars[k]

			return v, ok
		},
	})

	cmd.SetArgs(args)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.ExecuteContext(context.Background())

	return stdout.String(), stderr.String(), err
}

func TestVersionCommand(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, nil, "version")
	if err != nil {
		t.Fatalf("version: %v", err)
	}

	if !strings.HasPrefix(stdout, "pivot ") {
		t.Errorf("stdout = %q, want it to start with %q", stdout, "pivot ")
	}
}

func TestVersionCommandJSON(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, nil, "version", "--json")
	if err != nil {
		t.Fatalf("version --json: %v", err)
	}

	var info struct {
		Version   string `json:"version"`
		GoVersion string `json:"goVersion"`
		Platform  string `json:"platform"`
	}

	if err := json.Unmarshal([]byte(stdout), &info); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout)
	}

	if info.Version == "" || info.GoVersion == "" || info.Platform == "" {
		t.Errorf("JSON is missing fields: %+v", info)
	}
}

func TestConfigShowDefaults(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, nil, "config", "show", "--format", "json")
	if err != nil {
		t.Fatalf("config show: %v", err)
	}

	var cfg struct {
		Server struct {
			Port int `json:"Port"`
		} `json:"Server"`
	}

	if err := json.Unmarshal([]byte(stdout), &cfg); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout)
	}

	if cfg.Server.Port != 8080 {
		t.Errorf("port = %d, want the default 8080", cfg.Server.Port)
	}
}

// The precedence chain, asserted through the real command tree rather than
// only at the config package boundary.
func TestConfigShowPrecedence(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "pivot.yaml")

	if err := os.WriteFile(path, []byte("server:\n  port: 1111\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Run("file", func(t *testing.T) {
		t.Parallel()

		stdout, _, err := run(t, nil, "config", "show", "--config", path, "--format", "yaml")
		if err != nil {
			t.Fatalf("config show: %v", err)
		}

		if !strings.Contains(stdout, "port: 1111") {
			t.Errorf("output does not show the file value:\n%s", stdout)
		}
	})

	t.Run("env beats file", func(t *testing.T) {
		t.Parallel()

		env := map[string]string{"PIVOT_SERVER_PORT": "2222"}

		stdout, _, err := run(t, env, "config", "show", "--config", path, "--format", "yaml")
		if err != nil {
			t.Fatalf("config show: %v", err)
		}

		if !strings.Contains(stdout, "port: 2222") {
			t.Errorf("env did not override the file:\n%s", stdout)
		}
	})

	t.Run("flag beats env", func(t *testing.T) {
		t.Parallel()

		env := map[string]string{"PIVOT_SERVER_PORT": "2222"}

		stdout, _, err := run(t, env,
			"config", "show", "--config", path, "--format", "yaml", "--port", "3333")
		if err != nil {
			t.Fatalf("config show: %v", err)
		}

		if !strings.Contains(stdout, "port: 3333") {
			t.Errorf("flag did not override env:\n%s", stdout)
		}
	})
}

// An untouched flag must not outrank an environment variable just because it
// has a zero value. This is the bug that Changed() exists to prevent.
func TestUnsetFlagDoesNotOverrideEnv(t *testing.T) {
	t.Parallel()

	env := map[string]string{"PIVOT_LOG_LEVEL": "debug"}

	stdout, _, err := run(t, env, "config", "show", "--format", "yaml")
	if err != nil {
		t.Fatalf("config show: %v", err)
	}

	if !strings.Contains(stdout, "level: debug") {
		t.Errorf("an unset --log-level clobbered the env value:\n%s", stdout)
	}
}

func TestConfigShowReportsSourceFile(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, nil, "config", "show", "--format", "yaml")
	if err != nil {
		t.Fatalf("config show: %v", err)
	}

	if !strings.Contains(stdout, "config file: (none)") {
		t.Errorf("output does not report the absent config file:\n%s", stdout)
	}
}

func TestConfigEnvListsVariables(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, nil, "config", "env")
	if err != nil {
		t.Fatalf("config env: %v", err)
	}

	for _, want := range []string{"PIVOT_SERVER_PORT", "PIVOT_LOG_LEVEL", "PIVOT_LOG_FORMAT"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output is missing %q:\n%s", want, stdout)
		}
	}
}

func TestInvalidConfigIsRejected(t *testing.T) {
	t.Parallel()

	_, _, err := run(t, map[string]string{"PIVOT_LOG_LEVEL": "verbose"}, "config", "show")
	if err == nil {
		t.Fatal("an invalid log level was accepted")
	}

	if !strings.Contains(err.Error(), "log.level") {
		t.Errorf("error %q does not name the offending field", err)
	}
}

func TestRootShowsHelp(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, nil)
	if err != nil {
		t.Fatalf("root command: %v", err)
	}

	for _, want := range []string{"serve", "version", "config", "precedence"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help output is missing %q:\n%s", want, stdout)
		}
	}
}

func TestUnknownCommandIsRejected(t *testing.T) {
	t.Parallel()

	_, _, err := run(t, nil, "nonexistent")
	if err == nil {
		t.Fatal("an unknown command was accepted")
	}
}
