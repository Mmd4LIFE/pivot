package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/config"
	"github.com/Mmd4LIFE/pivot/internal/logging"
)

// Server owns the HTTP listener and its lifecycle.
//
// Signal handling deliberately lives in the CLI layer: this type shuts down
// when its context is canceled, which keeps it testable without sending
// real signals to the test process.
type Server struct {
	cfg    config.ServerConfig
	log    *slog.Logger
	http   *http.Server
	router *Router
	checks []Check

	// tenantResolver attributes requests to an organization. Nil until Part 6
	// supplies sessions; the server is explicitly unscoped until then.
	tenantResolver TenantResolver

	// ready gates /readyz. It flips false the instant shutdown begins, before
	// draining starts, so a load balancer stops sending new work while
	// in-flight requests finish.
	ready atomic.Bool

	// addr is the resolved listen address, known only after binding. It
	// matters because tests bind port 0.
	addr atomic.Pointer[string]
}

// Option configures a [Server].
type Option func(*Server)

// WithCheck registers a readiness check. Checks run on every /readyz request.
func WithCheck(c Check) Option {
	return func(s *Server) { s.checks = append(s.checks, c) }
}

// WithTenantResolver attributes requests to an organization.
//
// Without it the API is unscoped, which is valid only until Part 6 supplies
// sessions — and is why this is an explicit option rather than a default.
func WithTenantResolver(tr TenantResolver) Option {
	return func(s *Server) { s.tenantResolver = tr }
}

// New builds a server. It does not bind a port; [Server.Run] does that.
func New(cfg config.ServerConfig, log *slog.Logger, opts ...Option) *Server {
	s := &Server{cfg: cfg, log: log}

	for _, opt := range opts {
		opt(s)
	}

	s.router = NewRouter(RouterConfig{
		Log:            log,
		CORS:           DefaultCORS(),
		TenantResolver: s.tenantResolver,
		Checks:         s.checks,
	})
	s.router.setReady(&s.ready)

	s.http = &http.Server{
		Handler:           s.router.Handler(),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout.Duration(),
		ReadTimeout:       cfg.ReadTimeout.Duration(),
		WriteTimeout:      cfg.WriteTimeout.Duration(),
		IdleTimeout:       cfg.IdleTimeout.Duration(),
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
		BaseContext: func(net.Listener) context.Context {
			return logging.WithLogger(context.Background(), log)
		},
	}

	return s
}

// Addr returns the bound address, or "" before [Server.Run] has bound one.
func (s *Server) Addr() string {
	if p := s.addr.Load(); p != nil {
		return *p
	}

	return ""
}

// Run binds the listener and serves until ctx is canceled, then drains
// in-flight requests.
//
// It returns nil on a clean shutdown. A drain that exceeds
// ShutdownTimeout returns an error, because silently dropping requests is
// exactly the failure this whole path exists to prevent.
func (s *Server) Run(ctx context.Context) error {
	var lc net.ListenConfig

	ln, err := lc.Listen(ctx, "tcp", s.cfg.Address())
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.cfg.Address(), err)
	}

	resolved := ln.Addr().String()
	s.addr.Store(&resolved)
	s.ready.Store(true)

	s.log.Info("server listening",
		slog.String("addr", resolved),
		slog.String("healthz", "/healthz"),
		slog.String("readyz", "/readyz"),
	)

	serveErr := make(chan error, 1)

	go func() {
		// Serve always returns non-nil; ErrServerClosed means we asked it to stop.
		if err := s.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err

			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		s.ready.Store(false)

		if err != nil {
			return fmt.Errorf("serve: %w", err)
		}

		return nil

	case <-ctx.Done():
		return s.shutdown(ctx, serveErr)
	}
}

// shutdown stops accepting connections and drains what is in flight.
func (s *Server) shutdown(ctx context.Context, serveErr <-chan error) error {
	// Fail readiness first, then keep serving for the lame-duck period so a
	// load balancer actually observes the 503 and stops routing here. Calling
	// Shutdown immediately would refuse the probe's connection instead, which
	// looks like a network blip rather than a deliberate withdrawal.
	s.ready.Store(false)

	if delay := s.cfg.PreShutdownDelay.Duration(); delay > 0 {
		s.log.Info("draining: readiness failed, still accepting",
			slog.String("lame_duck", delay.String()))
		time.Sleep(delay)
	}

	timeout := s.cfg.ShutdownTimeout.Duration()
	s.log.Info("shutting down", slog.String("timeout", timeout.String()))

	// WithoutCancel keeps the parent's values — trace IDs and the logger — while
	// dropping the cancellation that triggered this shutdown. Deriving directly
	// from ctx would expire immediately and defeat the drain entirely.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	if err := s.http.Shutdown(shutdownCtx); err != nil {
		// Close() is the only remaining option; connections are severed.
		_ = s.http.Close()

		return fmt.Errorf("graceful shutdown exceeded %s: %w", timeout, err)
	}

	if err := <-serveErr; err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	s.log.Info("shutdown complete")

	return nil
}
