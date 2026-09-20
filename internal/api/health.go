package api

import (
	"context"
	"encoding/json"
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
func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	writeJSON(r.Context(), w, http.StatusOK, healthResponse{
		Status:  "ok",
		Version: version.Get().Version,
	})
}

// handleReady answers the readiness probe.
//
// Readiness means "send this instance traffic". It reports not-ready while
// shutting down and whenever a registered check fails.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if !s.ready.Load() {
		writeJSON(r.Context(), w, http.StatusServiceUnavailable, healthResponse{
			Status:  "shutting_down",
			Version: version.Get().Version,
		})

		return
	}

	results := make(map[string]string, len(s.checks))
	status := http.StatusOK
	overall := "ok"

	for _, check := range s.checks {
		if err := check.Func(r.Context()); err != nil {
			results[check.Name] = "error: " + err.Error()
			status = http.StatusServiceUnavailable
			overall = "not_ready"

			logging.FromContext(r.Context()).Warn("readiness check failed",
				slog.String("check", check.Name),
				logging.Err(err),
			)

			continue
		}

		results[check.Name] = "ok"
	}

	writeJSON(r.Context(), w, status, healthResponse{
		Status:  overall,
		Version: version.Get().Version,
		Checks:  results,
	})
}

// writeJSON writes a JSON response. Part 5 replaces this with the standard
// error envelope and its machine-readable codes.
func writeJSON(ctx context.Context, w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line is already sent, so this can only be logged.
		logging.FromContext(ctx).Error("write response", logging.Err(err))
	}
}
