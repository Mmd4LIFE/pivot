package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/api"
	"github.com/Mmd4LIFE/pivot/internal/config"
)

func testConfig() config.ServerConfig {
	cfg := config.Default().Server
	cfg.Host = "127.0.0.1"
	cfg.Port = 0 // let the kernel pick a free port
	cfg.ShutdownTimeout = config.Duration(5 * time.Second)

	return cfg
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// startServer runs a server and returns its base URL plus a stop function
// that shuts it down and reports the Run error.
func startServer(t *testing.T, opts ...api.Option) (string, func() error) {
	t.Helper()

	srv := api.New(testConfig(), discardLogger(), opts...)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() { errCh <- srv.Run(ctx) }()

	addr := waitForAddr(t, srv)

	stop := func() error {
		cancel()

		select {
		case err := <-errCh:
			return err
		case <-time.After(10 * time.Second):
			return errors.New("server did not shut down within 10s")
		}
	}

	t.Cleanup(func() { _ = stop() })

	return "http://" + addr, stop
}

func waitForAddr(t *testing.T, srv *api.Server) string {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if addr := srv.Addr(); addr != "" {
			return addr
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatal("server did not bind within 5s")

	return ""
}

func getJSON(t *testing.T, url string) (int, map[string]any) {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}

	return resp.StatusCode, body
}

func TestHealthzReportsOK(t *testing.T) {
	t.Parallel()

	base, _ := startServer(t)

	status, body := getJSON(t, base+"/healthz")
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}

	if body["status"] != "ok" {
		t.Errorf("status field = %v, want %q", body["status"], "ok")
	}
}

func TestReadyzReportsOK(t *testing.T) {
	t.Parallel()

	base, _ := startServer(t)

	status, body := getJSON(t, base+"/readyz")
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}

	if body["status"] != "ok" {
		t.Errorf("status field = %v, want %q", body["status"], "ok")
	}
}

// A failing dependency must fail readiness but never liveness: killing every
// pod because the database blinked turns a recoverable outage into a total one.
func TestFailingCheckFailsReadinessNotLiveness(t *testing.T) {
	t.Parallel()

	base, _ := startServer(t, api.WithCheck(api.Check{
		Name: "database",
		Func: func(context.Context) error { return errors.New("connection refused") },
	}))

	if status, _ := getJSON(t, base+"/healthz"); status != http.StatusOK {
		t.Errorf("liveness status = %d, want 200 despite the failing check", status)
	}

	status, body := getJSON(t, base+"/readyz")
	if status != http.StatusServiceUnavailable {
		t.Errorf("readiness status = %d, want 503", status)
	}

	if body["status"] != "not_ready" {
		t.Errorf("status field = %v, want %q", body["status"], "not_ready")
	}

	checks, ok := body["checks"].(map[string]any)
	if !ok {
		t.Fatalf("checks field = %v, want an object", body["checks"])
	}

	if got, _ := checks["database"].(string); got == "ok" || got == "" {
		t.Errorf("checks.database = %q, want an error description", got)
	}
}

func TestPassingCheckKeepsReadiness(t *testing.T) {
	t.Parallel()

	base, _ := startServer(t, api.WithCheck(api.Check{
		Name: "database",
		Func: func(context.Context) error { return nil },
	}))

	status, body := getJSON(t, base+"/readyz")
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}

	checks, ok := body["checks"].(map[string]any)
	if !ok {
		t.Fatalf("checks field = %v, want an object", body["checks"])
	}

	if checks["database"] != "ok" {
		t.Errorf("checks.database = %v, want %q", checks["database"], "ok")
	}
}

func TestShutdownIsClean(t *testing.T) {
	t.Parallel()

	_, stop := startServer(t)

	if err := stop(); err != nil {
		t.Fatalf("Run returned %v on a clean shutdown; want nil", err)
	}
}

func TestRunFailsOnUnavailablePort(t *testing.T) {
	t.Parallel()

	// Bind a port, then try to bind it again.
	first := api.New(testConfig(), discardLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = first.Run(ctx) }()

	addr := waitForAddr(t, first)

	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split %q: %v", addr, err)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port %q: %v", portStr, err)
	}

	cfg := testConfig()
	cfg.Port = port

	second := api.New(cfg, discardLogger())

	if err := second.Run(context.Background()); err == nil {
		t.Fatal("Run returned nil when the port was already bound")
	}
}
