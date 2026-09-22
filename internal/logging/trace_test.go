package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/Mmd4LIFE/pivot/internal/config"
	"github.com/Mmd4LIFE/pivot/internal/logging"
)

// withSpan returns a context carrying a valid, known span context.
func withSpan(t *testing.T) (context.Context, string, string) {
	t.Helper()

	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	if err != nil {
		t.Fatalf("trace id: %v", err)
	}

	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	if err != nil {
		t.Fatalf("span id: %v", err)
	}

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})

	return trace.ContextWithSpanContext(context.Background(), sc),
		traceID.String(), spanID.String()
}

func decode(t *testing.T, line string) map[string]any {
	t.Helper()

	var out map[string]any
	if err := json.Unmarshal([]byte(line), &out); err != nil {
		t.Fatalf("log line is not JSON: %v\nline: %s", err, line)
	}

	return out
}

func TestALogLineCarriesItsTrace(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := logging.WithTrace(logging.New(config.LogConfig{Level: "info"}, &buf))

	ctx, traceID, spanID := withSpan(t)
	log.InfoContext(ctx, "something happened")

	fields := decode(t, buf.String())

	if fields["trace_id"] != traceID {
		t.Errorf("trace_id = %v, want %s", fields["trace_id"], traceID)
	}

	if fields["span_id"] != spanID {
		t.Errorf("span_id = %v, want %s", fields["span_id"], spanID)
	}
}

func TestNoSpanMeansNoEmptyFields(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := logging.WithTrace(logging.New(config.LogConfig{Level: "info"}, &buf))

	// A background sweep, or tracing switched off entirely. An empty trace_id
	// on every line would be noise, and would make searching for a real one
	// harder rather than easier.
	log.InfoContext(context.Background(), "something happened")

	fields := decode(t, buf.String())

	if _, ok := fields["trace_id"]; ok {
		t.Errorf("trace_id is present with no span: %s", buf.String())
	}
}

// The failure this guards against is silent: a derived logger that has lost
// correlation still logs perfectly well, it is just no longer connected to
// anything. Almost every logger in the codebase is derived.
func TestADerivedLoggerKeepsItsCorrelation(t *testing.T) {
	t.Parallel()

	ctx, traceID, _ := withSpan(t)

	t.Run("With", func(t *testing.T) {
		var buf bytes.Buffer

		log := logging.WithTrace(logging.New(config.LogConfig{Level: "info"}, &buf)).
			With(slog.String("component", "auth"))

		log.InfoContext(ctx, "derived")

		fields := decode(t, buf.String())

		if fields["trace_id"] != traceID {
			t.Errorf("trace_id = %v, want %s", fields["trace_id"], traceID)
		}

		if fields["component"] != "auth" {
			t.Errorf("the attribute was lost: %s", buf.String())
		}
	})

	t.Run("WithGroup", func(t *testing.T) {
		var buf bytes.Buffer

		log := logging.WithTrace(logging.New(config.LogConfig{Level: "info"}, &buf)).
			WithGroup("request")

		log.InfoContext(ctx, "grouped", slog.String("path", "/healthz"))

		if !strings.Contains(buf.String(), traceID) {
			t.Errorf("trace_id is missing after WithGroup: %s", buf.String())
		}
	})
}

func TestTheConfiguredFormatIsKept(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	// Wrapping must not quietly convert text logs to JSON.
	log := logging.WithTrace(logging.New(config.LogConfig{Level: "info", Format: "text"}, &buf))

	ctx, traceID, _ := withSpan(t)
	log.InfoContext(ctx, "textual")

	line := buf.String()

	if strings.HasPrefix(strings.TrimSpace(line), "{") {
		t.Errorf("the text format became JSON: %s", line)
	}

	if !strings.Contains(line, traceID) {
		t.Errorf("trace_id is missing: %s", line)
	}
}
