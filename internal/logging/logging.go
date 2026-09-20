// Package logging builds the application's [slog.Logger] from configuration.
//
// Everything Pivot logs is structured. There is no fmt.Println path, because
// logs are read by machines — aggregators, alerting, the query log — far more
// often than by a person tailing a terminal. Trace correlation is added in
// Part 14 by wrapping the handler built here.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/Mmd4LIFE/pivot/internal/config"
)

// New builds a logger from configuration, writing to w.
//
// The configuration is assumed valid; [config.Config.Validate] runs before
// this is ever called. An unrecognized level or format still degrades to a
// sane default rather than failing, because losing logs is a bad way to learn
// about a config problem.
func New(cfg config.LogConfig, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level:     parseLevel(cfg.Level),
		AddSource: cfg.AddSource,
	}

	var handler slog.Handler
	if strings.EqualFold(cfg.Format, "text") {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}

	return slog.New(handler)
}

// parseLevel maps a configured level name to a [slog.Level], defaulting to
// info for anything unrecognized.
func parseLevel(name string) slog.Level {
	switch strings.ToLower(name) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// contextKey is unexported so nothing outside this package can collide with it.
type contextKey struct{}

// WithLogger returns a context carrying l.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, l)
}

// FromContext returns the logger stored in ctx, or [slog.Default] when there
// is none. It never returns nil, so callers can log unconditionally.
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(contextKey{}).(*slog.Logger); ok && l != nil {
		return l
	}

	return slog.Default()
}

// Err renders an error as a log attribute. Centralized so that error logging
// is consistent, and so redaction can be added in one place later.
func Err(err error) slog.Attr {
	if err == nil {
		return slog.Attr{}
	}

	return slog.String("error", err.Error())
}

// Addr renders a network address attribute.
func Addr(network, addr string) slog.Attr {
	return slog.String("addr", fmt.Sprintf("%s://%s", network, addr))
}
