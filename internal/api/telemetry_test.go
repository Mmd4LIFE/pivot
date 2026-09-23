package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/api"
	"github.com/Mmd4LIFE/pivot/internal/logging"
)

/*
The browser error endpoint.

It is unauthenticated, it writes to the log, and anyone can reach it — which
is three properties that are fine individually and need to be tested together.
Each test below is one way that combination goes wrong.
*/

const reportPath = api.APIPrefix + "/telemetry/errors"

// telemetryFixture builds a router with only the reporting endpoint, and a log
// to read back.
//
// No database: this endpoint touches none, and a fixture that opened one would
// be asserting something other than what the handler does.
type telemetryFixture struct {
	handler http.Handler
	logs    *bytes.Buffer
}

func newTelemetryFixture(t *testing.T) *telemetryFixture {
	t.Helper()

	logs := &bytes.Buffer{}
	log := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	router := api.NewRouter(api.RouterConfig{
		Log:       log,
		Telemetry: api.NewTelemetryHandler(log),
	})

	return &telemetryFixture{handler: router.Handler(), logs: logs}
}

// post sends a report and returns the response.
//
// The logger goes in through the context the way the server puts it there, so
// the handler's line lands in the buffer rather than on stderr.
func (f *telemetryFixture) post(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()

	return f.postWith(t, body, "application/json")
}

func (f *telemetryFixture) postWith(t *testing.T, body, contentType string) *httptest.ResponseRecorder {
	t.Helper()

	logger := slog.New(slog.NewJSONHandler(f.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := logging.WithLogger(context.Background(), logger)

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, reportPath, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)

	return rec
}

// logged returns the first record whose message is "browser error".
func (f *telemetryFixture) logged(t *testing.T) map[string]any {
	t.Helper()

	for line := range strings.SplitSeq(strings.TrimSpace(f.logs.String()), "\n") {
		if line == "" {
			continue
		}

		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line is not JSON: %v\n%s", err, line)
		}

		if record["msg"] == "browser error" {
			return record
		}
	}

	t.Fatalf("no browser error was logged\n%s", f.logs.String())

	return nil
}

// The whole point of the part: something thrown in a browser turns up in the
// server's log, carrying the trace of the API call that caused it.
func TestABrowserErrorReachesTheLogWithItsTrace(t *testing.T) {
	t.Parallel()

	f := newTelemetryFixture(t)

	rec := f.post(t, `{
		"kind": "render",
		"message": "Cannot read properties of undefined (reading 'name')",
		"stack": "at Dashboard (index-abc.js:2:1)",
		"url": "https://pivot.example.com/dashboards/1",
		"traceId": "4bf92f3577b34da6a3ce929d0e0e4736",
		"requestId": "0199a1f2-1c2a-7000-8000-000000000001"
	}`)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204\nbody: %s", rec.Code, rec.Body.String())
	}

	// Nothing comes back. The endpoint is unauthenticated, so a response that
	// described what was recorded would be a way to probe the instance.
	if rec.Body.Len() != 0 {
		t.Errorf("the response has a body: %s", rec.Body.String())
	}

	record := f.logged(t)

	for field, want := range map[string]string{
		"kind":               "render",
		"browser_message":    "Cannot read properties of undefined (reading 'name')",
		"browser_stack":      "at Dashboard (index-abc.js:2:1)",
		"browser_url":        "https://pivot.example.com/dashboards/1",
		"browser_trace_id":   "4bf92f3577b34da6a3ce929d0e0e4736",
		"browser_request_id": "0199a1f2-1c2a-7000-8000-000000000001",
	} {
		if got, _ := record[field].(string); got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}

	// Warn rather than Error: a browser error is worth seeing and is not this
	// instance failing. An alert rule on server errors should not fire because
	// somebody's extension broke a page.
	if level, _ := record["level"].(string); level != "WARN" {
		t.Errorf("level = %q, want WARN", level)
	}
}

// The trace ID goes in under its own key. Logging it as `trace_id` would let a
// browser overwrite the one this request actually has -- which is the field an
// operator searches by, and they are different traces in any case: one is the
// failure, the other is the report of it.
func TestAReportCannotForgeThisRequestsTrace(t *testing.T) {
	t.Parallel()

	f := newTelemetryFixture(t)

	f.post(t, `{"message": "boom", "traceId": "4bf92f3577b34da6a3ce929d0e0e4736"}`)

	record := f.logged(t)

	if _, ok := record["trace_id"]; ok {
		t.Errorf("the report set trace_id: %v", record["trace_id"])
	}

	if got, _ := record["browser_trace_id"].(string); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("browser_trace_id = %q", got)
	}
}

// A trace ID that is not one is dropped rather than logged. Anything that gets
// pasted into a trace viewer has to be findable there.
func TestAMalformedTraceIDIsDropped(t *testing.T) {
	t.Parallel()

	for name, id := range map[string]string{
		"too short":   "4bf92f35",
		"not hex":     "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz",
		"a sentence":  "trace id unknown",
		"with dashes": "4bf92f35-77b3-4da6-a3ce-929d0e0e4736",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newTelemetryFixture(t)

			f.post(t, `{"message": "boom", "traceId": `+quote(id)+`}`)

			if _, ok := f.logged(t)["browser_trace_id"]; ok {
				t.Errorf("a malformed trace ID was logged")
			}
		})
	}
}

// Everything in a report is attacker-controlled. A value with an escape
// sequence in it renders as whatever the sender wanted in a terminal reading
// the log, and one with a newline can forge a second line in any format that
// is not JSON.
func TestControlCharactersCannotReachTheLog(t *testing.T) {
	t.Parallel()

	f := newTelemetryFixture(t)

	f.post(t, `{"message": "red\u001b[31m alert\u0000 here"}`)

	message, _ := f.logged(t)["browser_message"].(string)

	if strings.ContainsAny(message, "\x00\x1b") {
		t.Errorf("control characters survived: %q", message)
	}

	if message != "red[31m alert here" {
		t.Errorf("message = %q", message)
	}
}

// A stack trace keeps its newlines -- without them it is one unreadable line,
// and slog escapes them in both its handlers.
func TestAStackKeepsItsLineBreaks(t *testing.T) {
	t.Parallel()

	f := newTelemetryFixture(t)

	f.post(t, `{"message": "boom", "stack": "at a (x.js:1:1)\nat b (x.js:2:2)"}`)

	stack, _ := f.logged(t)["browser_stack"].(string)

	if !strings.Contains(stack, "\n") {
		t.Errorf("the stack lost its line breaks: %q", stack)
	}
}

// Truncation, not rejection. A report that is slightly too verbose still names
// the error; a rejected one tells nobody anything.
func TestAnOversizedFieldIsTruncatedRatherThanRefused(t *testing.T) {
	t.Parallel()

	f := newTelemetryFixture(t)

	rec := f.post(t, `{"message": "`+strings.Repeat("a", 4000)+`"}`)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}

	message, _ := f.logged(t)["browser_message"].(string)

	if len(message) > 1024 {
		t.Errorf("message is %d bytes, want it capped at 1024", len(message))
	}

	if message == "" {
		t.Error("the message was dropped entirely")
	}
}

// The transport cap is separate and much smaller than the API's general 1 MiB.
// An unauthenticated endpoint that accepts a megabyte is somewhere to put data.
func TestAnOversizedBodyIsRefused(t *testing.T) {
	t.Parallel()

	f := newTelemetryFixture(t)

	rec := f.post(t, `{"message": "x", "stack": "`+strings.Repeat("a", api.MaxErrorReportBytes)+`"}`)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413\nbody: %s", rec.Code, rec.Body.String())
	}

	if code := decodeErrorBody(t, rec).Code; code != api.CodePayloadTooLarge {
		t.Errorf("code = %s, want %s", code, api.CodePayloadTooLarge)
	}
}

// A report with nothing in it is not a report. Accepting it would fill a log
// with lines that say an error happened and nothing else.
func TestAReportMustSayWhatTheErrorWas(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"absent":                 `{"kind": "render"}`,
		"empty":                  `{"message": ""}`,
		"whitespace":             `{"message": "   \n  "}`,
		"all control characters": `{"message": "\u0000\u0001"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := newTelemetryFixture(t).post(t, body)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("status = %d, want 422\nbody: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// Unknown fields are ignored here and rejected everywhere else in this API.
// The client is a cached bundle in somebody's browser and may be older or
// newer than this server; losing its report over a field this version does not
// know about would lose the report at exactly the moment a deploy went wrong.
func TestAnUnknownFieldDoesNotLoseTheReport(t *testing.T) {
	t.Parallel()

	f := newTelemetryFixture(t)

	rec := f.post(t, `{"message": "boom", "breadcrumbs": ["a", "b"], "release": "v2"}`)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204\nbody: %s", rec.Code, rec.Body.String())
	}

	if got, _ := f.logged(t)["browser_message"].(string); got != "boom" {
		t.Errorf("browser_message = %q", got)
	}
}

func TestAReportMustBeJSON(t *testing.T) {
	t.Parallel()

	t.Run("wrong content type", func(t *testing.T) {
		t.Parallel()

		rec := newTelemetryFixture(t).postWith(t, `{"message": "boom"}`, "text/plain")

		if rec.Code != http.StatusUnsupportedMediaType {
			t.Errorf("status = %d, want 415", rec.Code)
		}
	})

	t.Run("not JSON", func(t *testing.T) {
		t.Parallel()

		rec := newTelemetryFixture(t).post(t, `{"message": `)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400\nbody: %s", rec.Code, rec.Body.String())
		}
	})
}

// The endpoint is reachable by anyone, so the limiter is the whole defense
// against using it to fill a disk. The burst has to be big enough for the
// three events one broken render fires, and the sustained rate small.
func TestReportingIsRateLimited(t *testing.T) {
	t.Parallel()

	f := newTelemetryFixture(t)

	var throttled int

	// Twice the burst, so the second half has to be refused.
	for i := range 2 * api.LimitTelemetry.Burst {
		rec := f.post(t, `{"message": "boom `+itoa(i)+`"}`)
		if rec.Code == http.StatusTooManyRequests {
			throttled++
		}
	}

	if throttled == 0 {
		t.Fatal("nothing was throttled; the endpoint is unlimited")
	}

	if throttled > api.LimitTelemetry.Burst {
		t.Errorf("%d of %d requests were throttled, which is more than the burst allows",
			throttled, 2*api.LimitTelemetry.Burst)
	}
}

// One broken render fires a render error, a window.onerror and a rejected
// promise within the same frame. A limit that only let the first through would
// hide the two that explain it.
func TestOneBrokenRenderFitsInTheBurst(t *testing.T) {
	t.Parallel()

	f := newTelemetryFixture(t)

	for _, kind := range []string{"render", "error", "unhandledrejection"} {
		rec := f.post(t, `{"kind": "`+kind+`", "message": "boom"}`)
		if rec.Code != http.StatusNoContent {
			t.Errorf("%s report: status = %d, want 204", kind, rec.Code)
		}
	}
}

// Without a handler the route does not exist at all, and the frontend's
// reports get the standard coded 404 rather than a silent 200 from a
// catch-all.
func TestNoHandlerMeansNoEndpoint(t *testing.T) {
	t.Parallel()

	router := api.NewRouter(api.RouterConfig{Log: discardLogger()})

	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, reportPath, strings.NewReader(`{"message":"boom"}`))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	router.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	if code := decodeErrorBody(t, rec).Code; code != api.CodeNotFound {
		t.Errorf("code = %s, want %s", code, api.CodeNotFound)
	}
}

func quote(s string) string {
	b, _ := json.Marshal(s)

	return string(b)
}

func itoa(i int) string {
	b, _ := json.Marshal(i)

	return string(b)
}
