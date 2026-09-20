package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/config"
	"github.com/Mmd4LIFE/pivot/internal/logging"
)

func TestNewEmitsJSONByDefault(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := logging.New(config.Default().Log, &buf)

	log.Info("hello", slog.String("key", "value"))

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, buf.String())
	}

	if rec["msg"] != "hello" {
		t.Errorf("msg = %v, want %q", rec["msg"], "hello")
	}

	if rec["key"] != "value" {
		t.Errorf("key = %v, want %q", rec["key"], "value")
	}
}

func TestNewEmitsTextWhenConfigured(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	cfg := config.Default().Log
	cfg.Format = "text"

	logging.New(cfg, &buf).Info("hello")

	out := buf.String()
	if json.Valid(buf.Bytes()) {
		t.Errorf("output is JSON, want text:\n%s", out)
	}

	if !strings.Contains(out, "hello") {
		t.Errorf("output is missing the message:\n%s", out)
	}
}

func TestLevelFiltering(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		level      string
		wantDebug  bool
		wantInfo   bool
		wantErrors bool
	}{
		"debug": {"debug", true, true, true},
		"info":  {"info", false, true, true},
		"warn":  {"warn", false, false, true},
		"error": {"error", false, false, true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			cfg := config.Default().Log
			cfg.Level = tc.level

			log := logging.New(cfg, &buf)
			log.Debug("dbg")
			log.Info("inf")
			log.Error("err")

			out := buf.String()

			if got := strings.Contains(out, "dbg"); got != tc.wantDebug {
				t.Errorf("debug emitted = %v, want %v", got, tc.wantDebug)
			}

			if got := strings.Contains(out, "inf"); got != tc.wantInfo {
				t.Errorf("info emitted = %v, want %v", got, tc.wantInfo)
			}

			if got := strings.Contains(out, "err"); got != tc.wantErrors {
				t.Errorf("error emitted = %v, want %v", got, tc.wantErrors)
			}
		})
	}
}

func TestFromContextRoundTrip(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := logging.New(config.Default().Log, &buf)

	ctx := logging.WithLogger(context.Background(), log)

	logging.FromContext(ctx).Info("via context")

	if !strings.Contains(buf.String(), "via context") {
		t.Errorf("logger did not round-trip through the context:\n%s", buf.String())
	}
}

// FromContext must never return nil, so callers can log unconditionally.
func TestFromContextFallsBackToDefault(t *testing.T) {
	t.Parallel()

	if logging.FromContext(context.Background()) == nil {
		t.Fatal("FromContext returned nil for a bare context")
	}
}

func TestErrAttr(t *testing.T) {
	t.Parallel()

	attr := logging.Err(errors.New("boom"))
	if attr.Value.String() != "boom" {
		t.Errorf("Err value = %q, want %q", attr.Value.String(), "boom")
	}

	if empty := logging.Err(nil); empty.Key != "" {
		t.Errorf("Err(nil) produced key %q, want an empty attr", empty.Key)
	}
}
