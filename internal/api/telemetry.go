package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"unicode"

	"github.com/Mmd4LIFE/pivot/internal/logging"
)

// MaxErrorReportBytes bounds a browser error report.
//
// Sixteen kilobytes holds a message and a deep stack with room to spare, and
// refuses anything that is trying to use an unauthenticated endpoint as a
// place to put data. The general limit is 1 MiB, which is the wrong size for a
// route that anybody on the internet can reach without logging in.
const MaxErrorReportBytes = 16 << 10

// Field caps, applied after decoding.
//
// The transport limit above stops a large body; these stop a body that is
// within the limit but still unreasonable — 8 KiB of "message" in every log
// line is how a log sink fills up. Truncation rather than rejection is
// deliberate: a truncated report still names the error, and a rejected one
// tells nobody anything.
const (
	maxReportMessage = 1 << 10
	maxReportStack   = 8 << 10
	maxReportURL     = 512
	maxReportKind    = 32
)

// ErrorReport is what the browser posts when something throws.
//
// Every field is attacker-controlled, including from an authenticated user, so
// nothing here is trusted: strings are stripped of control characters and
// truncated before they reach a log, and TraceID is checked for shape rather
// than believed.
type ErrorReport struct {
	// Kind distinguishes a render error from a rejected promise from a plain
	// window.onerror. They fail differently and are usually different bugs.
	Kind string `json:"kind"`

	// Message is the error's own message.
	Message string `json:"message"`

	// Stack is whatever the browser could produce. Often absent: a
	// cross-origin script error gives "Script error." and nothing else.
	Stack string `json:"stack"`

	// URL is the page the error happened on, not the API call that failed.
	URL string `json:"url"`

	// TraceID is the trace of the API request that failed, when the error came
	// from one. The browser learns it from the `traceresponse` header the
	// tracing middleware sets -- it does not invent one, which is why an
	// operator can paste it into a trace viewer and find something.
	TraceID string `json:"traceId"`

	// RequestID is that same failed request's correlation ID.
	//
	// Both are here because they are not available at the same times. A trace
	// ID exists only when tracing is switched on, which by default it is not;
	// a request ID is on every response Pivot has ever sent. Reporting only
	// the trace would mean the default deployment learns nothing.
	RequestID string `json:"requestId"`
}

// TelemetryHandler receives error reports from the browser.
type TelemetryHandler struct {
	log *slog.Logger
}

// NewTelemetryHandler builds the handler.
func NewTelemetryHandler(log *slog.Logger) *TelemetryHandler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &TelemetryHandler{log: log}
}

// handleErrorReport records a browser error.
//
// It logs and returns 204. There is deliberately nothing else: no storage, no
// alerting, no aggregation. Those belong to whatever the operator already runs
// for their server logs, and a browser error that lands in the same place as
// the request that caused it is worth more than a second system to check.
//
// The response says nothing about what was recorded, because the endpoint is
// unauthenticated and a chatty one would be a way to probe the instance.
func (h *TelemetryHandler) handleErrorReport(w http.ResponseWriter, r *http.Request) {
	if err := requireJSON(r); err != nil {
		WriteError(w, r, err)

		return
	}

	// Wrapped before Decode would wrap it, so this tighter limit is the one
	// that fires.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxErrorReportBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			WriteError(w, r, Errorf(CodePayloadTooLarge,
				"An error report may be at most %d bytes", MaxErrorReportBytes))

			return
		}

		WriteError(w, r, NewError(CodeMalformedJSON, "The report could not be read", err))

		return
	}

	var report ErrorReport

	// Unknown fields are ignored here, unlike everywhere else in this API.
	// The client is a cached bundle in somebody's browser and may be older or
	// newer than this server; rejecting its report over a field this version
	// does not know about would lose the one thing the endpoint exists to
	// collect, at exactly the moment a deployment has gone wrong.
	if err := json.Unmarshal(body, &report); err != nil {
		WriteError(w, r, NewError(CodeMalformedJSON, "The report is not valid JSON", err))

		return
	}

	report.Message = clean(report.Message, maxReportMessage)
	if report.Message == "" {
		WriteError(w, r, ValidationError(Detail{
			Field: "message", Message: "An error report must say what the error was",
		}))

		return
	}

	attrs := []slog.Attr{
		slog.String("kind", clean(report.Kind, maxReportKind)),
		slog.String("browser_message", report.Message),
		slog.String("browser_url", clean(report.URL, maxReportURL)),
		slog.String("user_agent", r.UserAgent()),
	}

	if stack := clean(report.Stack, maxReportStack); stack != "" {
		attrs = append(attrs, slog.String("browser_stack", stack))
	}

	// Only a well-formed trace ID is logged, and under its own key. Reusing
	// `trace_id` would let a browser overwrite the one this request actually
	// has, which is the field an operator uses to find out what happened --
	// and the two are different traces anyway: one is the failure, the other
	// is the report of it.
	if isTraceID(report.TraceID) {
		attrs = append(attrs, slog.String("browser_trace_id", strings.ToLower(report.TraceID)))
	}

	// Sanitized the same way an inbound X-Request-Id is, and for the same
	// reason: it is going into a log line and it came from outside.
	if id := sanitizeRequestID(report.RequestID); id != "" {
		attrs = append(attrs, slog.String("browser_request_id", id))
	}

	// Warn, not Error. A browser error is real and worth seeing, but it is not
	// this instance failing -- an alert rule on server errors should not fire
	// because somebody's extension broke a page.
	logging.FromContext(r.Context()).LogAttrs(
		r.Context(), slog.LevelWarn, "browser error", attrs...)

	w.WriteHeader(http.StatusNoContent)
}

// clean makes a client-supplied string safe to put in a log line.
//
// Control characters go, because a log sink reading a value with an ANSI
// escape in it renders whatever the sender wanted, and a value with a newline
// in it can forge a second log line in any format that is not JSON. Tab and
// newline survive inside a stack trace, where they are the only thing making
// it readable, and both are escaped by slog's handlers.
func clean(s string, limit int) string {
	if len(s) > limit {
		// Cutting at a byte offset can land inside a multi-byte rune, so the
		// tail is dropped rather than left as a broken one. A log sink that
		// receives invalid UTF-8 either mangles the line or refuses it.
		s = strings.ToValidUTF8(s[:limit], "")
	}

	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case unicode.IsControl(r):
			return -1
		default:
			return r
		}
	}, s)

	return strings.TrimSpace(s)
}

// isTraceID reports whether s is a 32-character hex trace ID.
func isTraceID(s string) bool {
	if len(s) != 32 {
		return false
	}

	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}

	return true
}
