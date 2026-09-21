package api

import (
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/logging"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

// TenantResolver determines which organization a request belongs to.
//
// Part 6 replaces the resolver with one that reads the authenticated session.
// Keeping it an interface means that swap touches one constructor rather than
// the middleware, and lets tests drive the middleware without a session layer.
type TenantResolver interface {
	// Resolve returns the scope for a request, or an error if none applies.
	Resolve(*http.Request) (tenant.Scope, error)
}

// ResolverFunc adapts a function to [TenantResolver].
type ResolverFunc func(*http.Request) (tenant.Scope, error)

// Resolve implements [TenantResolver].
func (f ResolverFunc) Resolve(r *http.Request) (tenant.Scope, error) { return f(r) }

// SingleTenantResolver resolves every request to one organization.
//
// This is the stand-in until Part 6 provides real sessions. It is deliberately
// explicit rather than a default buried in the middleware: a single-tenant
// assumption that nobody can see is exactly the kind of thing that survives
// into a multi-tenant deployment.
func SingleTenantResolver(orgID uuid.UUID) TenantResolver {
	scope, err := tenant.NewSystemScope(orgID)

	return ResolverFunc(func(*http.Request) (tenant.Scope, error) {
		if err != nil {
			return tenant.Scope{}, err
		}

		return scope, nil
	})
}

// WithTenant resolves the tenant for each request and puts it in the context.
//
// A request whose tenant cannot be resolved is rejected here, with 401, and
// never reaches a handler. That ordering matters: the repository layer would
// also refuse it, but a 500 from a failed database call is the wrong answer to
// "who are you?", and relying on the inner layer to catch it means every new
// handler is one forgotten check away from running unscoped.
func WithTenant(resolver TenantResolver, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, err := resolver.Resolve(r)
			if err != nil {
				log.Warn("tenant not resolved",
					slog.String("path", r.URL.Path),
					logging.Err(err),
				)

				WriteError(w, r, &APIError{
					Code:    CodeTenantUnknown,
					Message: CodeTenantUnknown.Summary(),
					Err:     err,
				})

				return
			}

			ctx := tenant.WithScope(r.Context(), scope)

			// Carry the tenant into the logger so every line from this request
			// says which organization it belongs to.
			ctx = logging.WithLogger(ctx, log.With(
				slog.String("org_id", scope.OrgID().String()),
			))

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
