package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/Mmd4LIFE/pivot/internal/logging"
	"github.com/Mmd4LIFE/pivot/internal/version"
)

// Check is a named readiness probe. Part 3 registers the metadata database
// here; Part 7 registers the authorization backend.
type Check struct {
	Name string
	Func func(ctx context.Context) error
}

// healthResponse is the body of both probes.
type healthResponse struct {
	Status  string            `json:"status"`
	Version string            `json:"version"`
	Checks  map[string]string `json:"checks,omitempty"`
}

// handleLive answers the liveness probe.
//
// Liveness means "this process is running and should not be restarted". It
// deliberately does not consult dependencies: a database outage must not
// cause Kubernetes to kill every Pivot pod, which would turn a recoverable
// dependency failure into a total outage.
func (r *Router) handleLive(w http.ResponseWriter, req *http.Request) {
	WriteJSON(req.Context(), w, http.StatusOK, healthResponse{
		Status:  "ok",
		Version: version.Get().Version,
	})
}

// handleReady answers the readiness probe.
//
// Readiness means "send this instance traffic". It reports not-ready while
// shutting down and whenever a registered check fails.
func (r *Router) handleReady(w http.ResponseWriter, req *http.Request) {
	if !r.ready() {
		WriteJSON(req.Context(), w, http.StatusServiceUnavailable, healthResponse{
			Status:  "shutting_down",
			Version: version.Get().Version,
		})

		return
	}

	results := make(map[string]string, len(r.checks))
	status := http.StatusOK
	overall := "ok"

	for _, check := range r.checks {
		if err := check.Func(req.Context()); err != nil {
			results[check.Name] = "error: " + err.Error()
			status = http.StatusServiceUnavailable
			overall = "not_ready"

			logging.FromContext(req.Context()).Warn("readiness check failed",
				slog.String("check", check.Name),
				logging.Err(err),
			)

			continue
		}

		results[check.Name] = "ok"
	}

	WriteJSON(req.Context(), w, status, healthResponse{
		Status:  overall,
		Version: version.Get().Version,
		Checks:  results,
	})
}
