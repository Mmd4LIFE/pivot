package observability_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/Mmd4LIFE/pivot/internal/observability"
)

/*
The dashboard, checked against the metrics that exist.

"Renders against real data" cannot be asserted without a Grafana, but the way a
reference dashboard actually rots can: somebody renames a metric, or changes a
label, and the dashboard keeps loading and shows four empty panels. Nobody
notices until an incident.

So this parses the dashboard, extracts every metric and label it queries, and
requires each to appear in a real scrape of a real registry. It is the half of
"does the dashboard work" that a test can own.
*/

const dashboardPath = "../../deploy/grafana/pivot-overview.json"

type dashboard struct {
	Title  string `json:"title"`
	UID    string `json:"uid"`
	Panels []struct {
		Title   string `json:"title"`
		Targets []struct {
			Expr string `json:"expr"`
		} `json:"targets"`
	} `json:"panels"`
	Templating struct {
		List []struct {
			Name  string `json:"name"`
			Query any    `json:"query"`
		} `json:"list"`
	} `json:"templating"`
}

func loadDashboard(t *testing.T) dashboard {
	t.Helper()

	raw, err := os.ReadFile(dashboardPath)
	if err != nil {
		t.Fatalf("read the dashboard: %v", err)
	}

	var d dashboard
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("the dashboard is not valid JSON: %v", err)
	}

	return d
}

// exposition returns what a scrape would see, after recording one of everything.
func exposition(t *testing.T) string {
	t.Helper()

	m, handler, shutdown, err := observability.SetupMetrics(
		observability.MetricsConfig{Enabled: true, ServiceName: "pivot-test"}, discard())
	if err != nil {
		t.Fatalf("setup metrics: %v", err)
	}

	t.Cleanup(func() { _ = shutdown(context.Background()) })

	// Recorded with the same attributes the HTTP middleware attaches, because
	// the labels are the whole point: a dashboard checked against an
	// unlabelled exposition would pass while filtering on labels nothing
	// carries.
	//
	// The keys are restated here rather than imported, because the middleware
	// lives in internal/api and importing it would be a cycle. That
	// duplication is the reason internal/api/metrics_test.go asserts the same
	// labels against the real router -- between them, a renamed key fails
	// somewhere.
	ctx := context.Background()
	attrs := metric.WithAttributes(
		attribute.String("http.method", "GET"),
		attribute.String("http.route", "/healthz"),
		observability.StatusClass(http.StatusOK),
	)

	m.Requests.Add(ctx, 1, attrs)
	m.Duration.Record(ctx, 0.1, attrs)
	m.InFlight.Add(ctx, 1, attrs)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(
		rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, observability.MetricsPath, http.NoBody))

	return rec.Body.String()
}

// pivotMetric matches a pivot_* series name inside a PromQL expression.
var pivotMetric = regexp.MustCompile(`pivot_[a-z_]+`)

func TestEveryDashboardQueryNamesAMetricThatExists(t *testing.T) {
	d := loadDashboard(t)
	body := exposition(t)

	if len(d.Panels) == 0 {
		t.Fatal("the dashboard has no panels")
	}

	seen := map[string]bool{}

	for _, panel := range d.Panels {
		if len(panel.Targets) == 0 {
			t.Errorf("panel %q has no query", panel.Title)
		}

		for _, target := range panel.Targets {
			for _, metric := range pivotMetric.FindAllString(target.Expr, -1) {
				seen[metric] = true

				// _bucket and _count are suffixes Prometheus adds to a
				// histogram, so the base name is what has to exist.
				base := strings.TrimSuffix(strings.TrimSuffix(metric, "_bucket"), "_count")

				if !strings.Contains(body, base) {
					t.Errorf("panel %q queries %q, which nothing exposes",
						panel.Title, metric)
				}
			}
		}
	}

	if len(seen) == 0 {
		t.Fatal("no panel queries a pivot metric at all")
	}

	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}

	sort.Strings(names)
	t.Logf("dashboard queries: %s", strings.Join(names, ", "))
}

func TestEveryDashboardLabelExists(t *testing.T) {
	d := loadDashboard(t)
	body := exposition(t)

	// A renamed label is the quieter failure: the panel still loads, the query
	// still parses, and it matches nothing.
	labels := regexp.MustCompile(`(http_[a-z_]+)\s*[=~]`)

	for _, panel := range d.Panels {
		for _, target := range panel.Targets {
			for _, match := range labels.FindAllStringSubmatch(target.Expr, -1) {
				label := match[1]

				// The templating variable, not a label.
				if strings.HasPrefix(label, "http_route") && strings.Contains(target.Expr, "by (http_route)") {
					continue
				}

				if !strings.Contains(body, label+"=") {
					t.Errorf("panel %q filters on %q, which no series carries",
						panel.Title, label)
				}
			}
		}
	}
}

func TestTheDashboardIsScopedToAnInstance(t *testing.T) {
	d := loadDashboard(t)

	// Several Pivots reporting into one Prometheus is the normal case. Without
	// an instance variable the panels add them together, and one unhealthy
	// instance disappears into the average of the healthy ones.
	found := false

	for _, v := range d.Templating.List {
		if v.Name == "instance" {
			found = true
		}
	}

	if !found {
		t.Error("the dashboard has no instance variable")
	}

	for _, panel := range d.Panels {
		for _, target := range panel.Targets {
			if !strings.Contains(target.Expr, "$instance") {
				t.Errorf("panel %q ignores the instance variable", panel.Title)
			}
		}
	}
}
