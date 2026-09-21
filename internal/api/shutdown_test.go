package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/config"
)

// This file is in package api (not api_test) so it can reach s.routes and
// install a slow handler. Draining is the behavior most worth testing and
// the hardest to observe from outside.

func drainTestServer(
	t *testing.T,
	lameDuck time.Duration,
	handler http.HandlerFunc,
) (*Server, string, func() error) {
	t.Helper()

	cfg := config.Default().Server
	cfg.Host = "127.0.0.1"
	cfg.Port = 0
	cfg.ShutdownTimeout = config.Duration(10 * time.Second)
	cfg.PreShutdownDelay = config.Duration(lameDuck)

	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	srv := New(cfg, log)

	// The server's handler already wraps this mux, so registering here adds
	// the endpoint to the live chain without rebuilding it.
	srv.router.Mux().HandleFunc("GET /slow", handler)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() { errCh <- srv.Run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for srv.Addr() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if srv.Addr() == "" {
		t.Fatal("server did not bind within 5s")
	}

	stop := func() error {
		cancel()

		select {
		case err := <-errCh:
			return err
		case <-time.After(15 * time.Second):
			t.Fatal("server did not shut down within 15s")

			return nil
		}
	}

	return srv, "http://" + srv.Addr(), stop
}

// The core guarantee: a request already in flight when SIGTERM arrives must
// complete, not be severed.
func TestShutdownDrainsInFlightRequests(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})

	// No lame-duck period: draining in-flight work must work regardless.
	_, base, stop := drainTestServer(t, 0, func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("finished"))
	})

	type result struct {
		status int
		body   string
		err    error
	}

	resCh := make(chan result, 1)

	go func() {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/slow", http.NoBody)
		if err != nil {
			resCh <- result{err: err}

			return
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			resCh <- result{err: err}

			return
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := io.ReadAll(resp.Body)
		resCh <- result{status: resp.StatusCode, body: string(body), err: err}
	}()

	<-started // the handler is now mid-flight

	shutdownDone := make(chan error, 1)

	go func() { shutdownDone <- stop() }()

	// Give shutdown a moment to begin while the request is still running.
	time.Sleep(100 * time.Millisecond)

	close(release) // let the handler finish

	res := <-resCh
	if res.err != nil {
		t.Fatalf("in-flight request failed during shutdown: %v", res.err)
	}

	if res.status != http.StatusOK {
		t.Errorf("in-flight request status = %d, want 200", res.status)
	}

	if res.body != "finished" {
		t.Errorf("in-flight response body = %q, want %q", res.body, "finished")
	}

	if err := <-shutdownDone; err != nil {
		t.Errorf("shutdown returned %v, want nil", err)
	}
}

// During the lame-duck period the server must still accept connections and
// answer /readyz with 503, so a load balancer observes a deliberate withdrawal
// rather than a connection refusal it would read as a transient blip.
func TestReadinessFailsDuringLameDuckPeriod(t *testing.T) {
	t.Parallel()

	const lameDuck = 2 * time.Second

	_, base, stop := drainTestServer(t, lameDuck, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Confirm readiness is healthy before shutdown begins.
	if status := probe(t, base+"/readyz"); status != http.StatusOK {
		t.Fatalf("pre-shutdown readiness = %d, want 200", status)
	}

	shutdownDone := make(chan error, 1)

	go func() { shutdownDone <- stop() }()

	// Poll on fresh connections. The listener is still open during the lame
	// duck window, so these must succeed and report 503.
	var gotUnavailable bool

	deadline := time.Now().Add(lameDuck)
	for time.Now().Before(deadline) {
		if probe(t, base+"/readyz") == http.StatusServiceUnavailable {
			gotUnavailable = true

			break
		}

		time.Sleep(20 * time.Millisecond)
	}

	if err := <-shutdownDone; err != nil {
		t.Errorf("shutdown returned %v, want nil", err)
	}

	if !gotUnavailable {
		t.Error("readiness never reported 503 while still accepting; " +
			"a load balancer would keep routing to a departing instance")
	}
}

// probe issues a GET and returns the status code, or 0 if the request failed.
func probe(t *testing.T, url string) int {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
	if err != nil {
		return 0
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	defer func() { _ = resp.Body.Close() }()

	return resp.StatusCode
}

// The lame-duck period must be opt-in: local Ctrl-C should be instant, not
// delayed by a feature that only matters behind a load balancer.
func TestZeroLameDuckShutsDownPromptly(t *testing.T) {
	t.Parallel()

	_, _, stop := drainTestServer(t, 0, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	start := time.Now()

	if err := stop(); err != nil {
		t.Fatalf("shutdown returned %v, want nil", err)
	}

	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("shutdown took %v with no in-flight work; want near-instant", elapsed)
	}
}

// A drain that cannot finish in time must report an error rather than
// pretending the shutdown was clean.
func TestShutdownTimeoutReportsError(t *testing.T) {
	t.Parallel()

	cfg := config.Default().Server
	cfg.Host = "127.0.0.1"
	cfg.Port = 0
	cfg.ShutdownTimeout = config.Duration(100 * time.Millisecond)

	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	srv := New(cfg, log)

	release := make(chan struct{})
	defer close(release)

	started := make(chan struct{})

	srv.router.Mux().HandleFunc("GET /stuck", func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() { errCh <- srv.Run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for srv.Addr() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	go func() {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet,
			"http://"+srv.Addr()+"/stuck", http.NoBody)

		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()

	<-started
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("Run returned nil after the drain exceeded its timeout; want an error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after shutdown timeout")
	}
}
