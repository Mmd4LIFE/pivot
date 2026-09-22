package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/api"
	"github.com/Mmd4LIFE/pivot/internal/observability"
	"github.com/Mmd4LIFE/pivot/internal/store"
)

/*
The metrics an operator watches, measured through the real router.

What matters here is not that a counter increments -- it is the labels. A
metric with unbounded cardinality does not fail, it degrades the monitoring
system that scrapes it, and it does so weeks later under someone else's name.
So the assertions are mostly about what is *not* in a label.
*/

// meteredFixture is a signed-in fixture whose router records metrics.
func meteredFixture(t *testing.T, db *store.DB) (*authFixture, http.Handler) {
	t.Helper()

	m, handler, shutdown, err := observability.SetupMetrics(
		observability.MetricsConfig{Enabled: true, ServiceName: "pivot-test"}, discardLogger())
	if err != nil {
		t.Fatalf("setup metrics: %v", err)
	}

	t.Cleanup(func() { _ = shutdown(t.Context()) })

	if handler == nil {
		t.Fatal("no metrics handler")
	}

	f := newAuthFixture(t, db, func(c *api.RouterConfig) {
		c.Metrics = m
		c.MetricsHandler = handler
	})

	if resp := f.login(t, fixtureEmail, fixturePassword); resp.status != http.StatusOK {
		t.Fatalf("login = %d: %s", resp.status, resp)
	}

	return f, handler
}

// scrape returns the exposition text.
func scrape(t *testing.T, f *authFixture) string {
	t.Helper()

	resp := f.request(t, http.MethodGet, observability.MetricsPath, nil)
	if resp.status != http.StatusOK {
		t.Fatalf("GET /metrics = %d: %s", resp.status, resp)
	}

	return string(resp.body)
}

func TestMetricsRecordRequestsThroughTheRealRouter(t *testing.T) {
	bothEngines(t, func(t *testing.T, db *store.DB) {
		f, _ := meteredFixture(t, db)

		if resp := f.request(t, http.MethodGet, api.APIPrefix+"/auth/me", nil); resp.status != http.StatusOK {
			t.Fatalf("GET /auth/me = %d: %s", resp.status, resp)
		}

		body := scrape(t, f)

		if !strings.Contains(body, "pivot_http_requests_total") {
			t.Fatalf("no request counter in the exposition:\n%s", body)
		}

		if !strings.Contains(body, "pivot_http_duration_seconds") {
			t.Error("no duration histogram in the exposition")
		}
	})
}

func TestTheRouteLabelIsAPatternAndNeverAPath(t *testing.T) {
	bothEngines(t, func(t *testing.T, db *store.DB) {
		f, _ := meteredFixture(t, db)

		// A path with an identifier in it. If the label were the path, this
		// single request would create a series that exists forever and is
		// never queried again -- and a product that works would create one per
		// user, per dashboard, per query.
		id := "01a0ca73-52d0-76e4-948c-ae598777e105"

		f.request(t, http.MethodDelete, api.APIPrefix+"/auth/sessions/"+id, nil)

		body := scrape(t, f)

		if strings.Contains(body, id) {
			t.Errorf("the identifier leaked into a label:\n%s", body)
		}

		// The pattern is what should be there instead.
		if !strings.Contains(body, "{id}") {
			t.Errorf("no route pattern in the labels:\n%s", body)
		}
	})
}

func TestAScannerCannotCreateSeries(t *testing.T) {
	bothEngines(t, func(t *testing.T, db *store.DB) {
		f, _ := meteredFixture(t, db)

		// Somebody scanning for admin panels. Labeling by path would let a
		// stranger create a series per guess and turn a probe into an outage
		// of whatever is scraping this.
		for _, guess := range []string{"/wp-admin", "/.env", "/phpmyadmin"} {
			f.request(t, http.MethodGet, guess, nil)
		}

		body := scrape(t, f)

		for _, guess := range []string{"wp-admin", ".env", "phpmyadmin"} {
			if strings.Contains(body, guess) {
				t.Errorf("%q became a label:\n%s", guess, body)
			}
		}

		// They are counted, under the catch-all pattern. This router registers
		// "/" -- the SPA fallback in production, the 404 handler otherwise --
		// so nothing is ever genuinely unmatched, and three guesses collapse
		// into one series rather than three.
		//
		// I first asserted that these arrive as "unmatched". They do not, and
		// the router was right: a catch-all is exactly what bounds this.
		if !strings.Contains(body, `http_route="/"`) {
			t.Errorf("the guesses were not counted under the catch-all:\n%s", body)
		}
	})
}

func TestErrorsAreALabelRatherThanASeparateCounter(t *testing.T) {
	bothEngines(t, func(t *testing.T, db *store.DB) {
		f, _ := meteredFixture(t, db)

		// A 404 through the API prefix.
		f.request(t, http.MethodGet, api.APIPrefix+"/nothing-here", nil)

		body := scrape(t, f)

		// Rate and error rate come from one counter filtered by status class,
		// which is what keeps them consistent by construction: two counters
		// can drift, one cannot disagree with itself.
		if !strings.Contains(body, `http_status_class="4xx"`) {
			t.Errorf("no status class label:\n%s", body)
		}

		if strings.Contains(body, "pivot_http_errors") {
			t.Error("errors are a separate counter; they should be a label")
		}
	})
}

func TestTheMetricsEndpointNeedsNoSession(t *testing.T) {
	bothEngines(t, func(t *testing.T, db *store.DB) {
		f, _ := meteredFixture(t, db)

		// Prometheus has no good way to hold a session, and every scrape would
		// otherwise cost an Argon2 verification. Keeping :8080 off the public
		// internet is the operator's job and already true of every other route.
		client := newTestClient(nil)

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
			f.server.URL+observability.MetricsPath, http.NoBody)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}

		if resp := send(t, client, req); resp.status != http.StatusOK {
			t.Errorf("unauthenticated scrape = %d, want 200", resp.status)
		}
	})
}
