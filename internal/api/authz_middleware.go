package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/logging"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

// RequirePermission refuses a request whose caller lacks a permission.
//
// It composes *after* the tenant chain, so by the time it runs the request has
// both an identity and a scope. That ordering is why this middleware does not
// have to handle "who are you?" — an anonymous caller was already refused with
// 401, and reaching here means the only open question is "may you?".
//
// The object checked is the caller's own organization. Phase 0 has no other
// object type; Phase 4 introduces per-resource checks, which belong in the
// handler where the resource ID is known rather than in a middleware that
// would have to guess it from the path.
//
// Note what this is not: it is not the enforcement point for row and column
// access. Those live in the query compiler, because a new surface must not be
// able to introduce a bypass by forgetting a middleware. See architectural
// rule 1 and ADR-0009.
func RequirePermission(checker authz.Checker, perm authz.Permission, log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, err := tenant.FromContext(r.Context())
			if err != nil {
				// Unreachable behind WithTenant, which rejects a scopeless
				// request before any of this runs. Checked anyway: a chain
				// someone reorders must fail closed, not open.
				WriteError(w, r, NewError(CodeTenantUnknown, CodeTenantUnknown.Summary(), err))

				return
			}

			identity, ok := IdentityFrom(r.Context())
			if !ok {
				WriteError(w, r, NewError(CodeUnauthorized, CodeUnauthorized.Summary(), ErrNoSession))

				return
			}

			req := authz.Request{
				Subject:    authz.User(identity.Session.UserID.String()),
				Permission: perm,
				Object: authz.Object{
					Type: authz.TypeOrganization,
					ID:   scope.OrgID().String(),
				},
			}

			if derr := authz.Enforce(r.Context(), checker, req); derr != nil {
				WriteError(w, r, permissionError(derr, req, log, r))

				return
			}

			next.ServeHTTP(w, r.WithContext(r.Context()))
		})
	}
}

// permissionError maps an authorization failure onto the error contract.
//
// The distinction it draws is the one that matters operationally. "You may
// not" is a 403 and a normal answer. "I could not find out" is a 503 and an
// outage — the caller may well be permitted, and telling them 403 would send
// them to argue with an administrator about a permission they already have.
// Both deny; only one is a bug.
func permissionError(err error, req authz.Request, log *slog.Logger, r *http.Request) error {
	if errors.Is(err, authz.ErrUnavailable) {
		logging.FromContext(r.Context()).Error("authorization decision unavailable",
			slog.String("permission", string(req.Permission)),
			slog.String("object", req.Object.String()),
			logging.Err(err),
		)

		return NewError(CodeUnavailable,
			"The authorization backend could not be reached", err)
	}

	// A denial is ordinary traffic, logged at debug so a probing scanner does
	// not become an alert storm.
	logging.FromContext(r.Context()).Debug("permission denied",
		slog.String("permission", string(req.Permission)),
		slog.String("subject", req.Subject.String()),
	)

	return &APIError{
		Code:    CodeForbidden,
		Message: "You do not have permission to " + string(req.Permission),
		Err:     err,
	}
}

// EffectivePermissions returns everything the caller may do in their
// organization.
//
// It asks the checker one question per permission rather than reading the role
// table directly, so the list a client is shown and the list actually enforced
// can never drift — they come from the same code path. The decision cache makes
// the repetition cheap.
//
// A permission that cannot be decided is omitted rather than assumed. The list
// is advisory — it exists so a UI can hide what it cannot do — and enforcement
// happens at the endpoint regardless.
func EffectivePermissions(r *http.Request, checker authz.Checker) []authz.Permission {
	// No checker configured means nothing can be said, not that everything is
	// permitted. Returning nil is also what stops this from panicking on a
	// deployment that has authentication but no authorization wired up.
	if checker == nil {
		return nil
	}

	scope, err := tenant.FromContext(r.Context())
	if err != nil {
		return nil
	}

	identity, ok := IdentityFrom(r.Context())
	if !ok {
		return nil
	}

	object := authz.Object{Type: authz.TypeOrganization, ID: scope.OrgID().String()}
	subject := authz.User(identity.Session.UserID.String())

	out := make([]authz.Permission, 0, len(authz.AllPermissions))

	for _, perm := range authz.AllPermissions {
		decision, derr := checker.Check(r.Context(), authz.Request{
			Subject: subject, Permission: perm, Object: object,
		})
		if derr != nil || !decision.Allowed {
			continue
		}

		out = append(out, perm)
	}

	return out
}
