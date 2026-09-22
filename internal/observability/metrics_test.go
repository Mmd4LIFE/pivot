package observability_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/observability"
)

/*
Note on parallelism: same as the tracing tests. SetupMetrics installs the
global meter provider, so these run in series.
*/

func TestDisabledMetricsStillGiveWorkingInstruments(t *testing.T) {
	m, _, shutdown, err := observability.SetupMetrics(
		observability.MetricsConfig{Enabled: false}, discard())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// The HTTP layer records unconditionally. If these were nil it would have
	// to branch on every request, and the branch would be wrong somewhere.
	if m == nil || m.Requests == nil || m.Duration == nil || m.InFlight == nil {
		t.Fatal("an instrument is nil with metrics disabled")
	}

	m.Requests.Add(context.Background(), 1)
	m.Duration.Record(context.Background(), 0.01)
	m.InFlight.Add(context.Background(), 1)

	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}

func TestDisabledMetricsServeNoEndpoint(t *testing.T) {
	_, handler, _, err := observability.SetupMetrics(
		observability.MetricsConfig{Enabled: false}, discard())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Nil, so the router leaves the route unregistered and a scrape gets a
	// 404. That is the truth -- this Pivot has metrics off. An empty 200 would
	// tell an operator their scrape is working when it is measuring nothing.
	if handler != nil {
		t.Error("a handler was returned with metrics disabled")
	}
}

func TestEnabledMetricsExposeWhatWasRecorded(t *testing.T) {
	m, handler, shutdown, err := observability.SetupMetrics(
		observability.MetricsConfig{Enabled: true, ServiceName: "pivot-test"}, discard())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	defer func() { _ = shutdown(context.Background()) }()

	m.Requests.Add(context.Background(), 3)
	m.Duration.Record(context.Background(), 0.125)

	if handler == nil {
		t.Fatal("no handler with metrics enabled")
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, observability.MetricsPath, http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	body := rec.Body.String()

	// Prometheus renames on the way out: dots become underscores and a
	// counter gains a _total suffix. Asserting the exposed name rather than
	// the instrument name is the point -- the exposed one is what a dashboard
	// query has to spell.
	for _, want := range []string{
		"pivot_http_requests_total",
		"pivot_http_duration_seconds",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the exposition does not contain %q", want)
		}
	}
}

func TestStatusClassBucketsRatherThanEnumerates(t *testing.T) {
	t.Parallel()

	cases := map[int]string{
		200: "2xx",
		204: "2xx",
		301: "3xx",
		404: "4xx",
		422: "4xx",
		500: "5xx",
		503: "5xx",
	}

	for status, want := range cases {
		// The label is the class, not the code. A label per status code
		// multiplies every series by the number of codes the application can
		// return, and no dashboard needs to tell 502 from 503 before it has
		// told errors from successes.
		if got := observability.StatusClass(status).Value.AsString(); got != want {
			t.Errorf("StatusClass(%d) = %s, want %s", status, got, want)
		}
	}
}
