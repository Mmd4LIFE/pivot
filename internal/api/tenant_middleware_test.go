package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/api"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

// A request whose tenant cannot be resolved must be rejected by the
// middleware, before a handler runs. The repository layer would also refuse
// it, but a database error is the wrong answer to "who are you?", and relying
// on the inner layer means every new handler is one forgotten check away from
// running unscoped.
func TestWithTenantRejectsUnresolvableRequests(t *testing.T) {
	t.Parallel()

	var handlerRan bool

	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		handlerRan = true
	})

	resolver := api.ResolverFunc(func(*http.Request) (tenant.Scope, error) {
		return tenant.Scope{}, errors.New("no session")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/users", http.NoBody)

	api.WithTenant(resolver, discardLogger())(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}

	if handlerRan {
		t.Error("the handler ran for a request with no resolvable tenant")
	}
}

// A resolved tenant must reach the handler through the context, which is the
// only way the repository layer can see it.
func TestWithTenantPutsScopeInContext(t *testing.T) {
	t.Parallel()

	orgID := uuid.Must(uuid.NewV7())

	var (
		got   tenant.Scope
		found bool
	)

	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		scope, err := tenant.FromContext(r.Context())
		if err != nil {
			t.Errorf("handler context has no scope: %v", err)

			return
		}

		got, found = scope, true
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/users", http.NoBody)

	api.WithTenant(api.SingleTenantResolver(orgID), discardLogger())(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	if !found {
		t.Fatal("the handler did not receive a scope")
	}

	if got.OrgID() != orgID {
		t.Errorf("scope org = %v, want %v", got.OrgID(), orgID)
	}
}

// A resolver that hands back a scope naming no organization must be rejected
// too, not passed through as "organization uuid.Nil".
func TestWithTenantRejectsInvalidScope(t *testing.T) {
	t.Parallel()

	var handlerRan bool

	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		handlerRan = true
	})

	// SingleTenantResolver validates at construction, so a nil organization
	// makes every request fail rather than silently succeeding.
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/users", http.NoBody)

	api.WithTenant(api.SingleTenantResolver(uuid.Nil), discardLogger())(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for a nil organization", rec.Code)
	}

	if handlerRan {
		t.Error("the handler ran with an invalid scope")
	}
}

// The middleware must not leak a scope between requests.
func TestWithTenantDoesNotLeakAcrossRequests(t *testing.T) {
	t.Parallel()

	orgA := uuid.Must(uuid.NewV7())
	orgB := uuid.Must(uuid.NewV7())

	seen := make([]uuid.UUID, 0, 2)

	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		scope, err := tenant.FromContext(r.Context())
		if err != nil {
			t.Errorf("no scope: %v", err)

			return
		}

		seen = append(seen, scope.OrgID())
	})

	for _, org := range []uuid.UUID{orgA, orgB} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/users", http.NoBody)

		api.WithTenant(api.SingleTenantResolver(org), discardLogger())(handler).ServeHTTP(rec, req)
	}

	if len(seen) != 2 || seen[0] != orgA || seen[1] != orgB {
		t.Errorf("scopes seen = %v, want [%v %v]", seen, orgA, orgB)
	}
}

// A bare context carries no scope, which is what makes the repository layer's
// fail-closed behavior reachable rather than theoretical.
func TestBareContextHasNoScope(t *testing.T) {
	t.Parallel()

	if tenant.Has(context.Background()) {
		t.Error("a background context reports a tenant scope")
	}
}
