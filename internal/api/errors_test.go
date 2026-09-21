package api_test

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/api"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

// codePattern is the published format. It is in the OpenAPI spec as a regex,
// so clients may rely on it.
var codePattern = regexp.MustCompile(`^PIVOT-[A-Z]+-\d{3}$`)

// Every registered code must be well-formed, unique, and carry a sensible
// status and summary. A code with no registry entry would silently fall back
// to 500, which is exactly the kind of quiet wrong answer this registry exists
// to prevent.
func TestCodeRegistryIsWellFormed(t *testing.T) {
	t.Parallel()

	seen := map[api.Code]bool{}

	for _, code := range api.RegisteredCodes() {
		if !codePattern.MatchString(string(code)) {
			t.Errorf("code %q does not match the published format %s", code, codePattern)
		}

		if seen[code] {
			t.Errorf("code %q is registered twice", code)
		}

		seen[code] = true

		if status := code.Status(); status < 400 || status > 599 {
			t.Errorf("code %q has status %d; error codes must map to 4xx or 5xx", code, status)
		}

		if summary := code.Summary(); summary == "" || summary == "Unknown error" {
			t.Errorf("code %q has no summary", code)
		}

		if !strings.HasPrefix(code.DocsURL(), api.DocsBaseURL) {
			t.Errorf("code %q has a malformed docs URL: %s", code, code.DocsURL())
		}

		if !code.Known() {
			t.Errorf("code %q reports itself unknown", code)
		}
	}

	if len(seen) < 10 {
		t.Errorf("only %d codes registered; the walk is not finding them", len(seen))
	}
}

// An unregistered code must not masquerade as a real one.
func TestUnknownCodeFallsBackSafely(t *testing.T) {
	t.Parallel()

	bogus := api.Code("PIVOT-NOPE-999")

	if bogus.Known() {
		t.Error("an unregistered code reports itself known")
	}

	if bogus.Status() != http.StatusInternalServerError {
		t.Errorf("unknown code status = %d, want 500", bogus.Status())
	}
}

// FromError is the single translation point between the layers below and HTTP.
func TestFromErrorMapsLayerErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err      error
		wantCode api.Code
	}{
		"not found":     {repo.ErrNotFound, api.CodeNotFound},
		"conflict":      {repo.ErrConflict, api.CodeVersionConflict},
		"duplicate":     {repo.ErrDuplicate, api.CodeDuplicate},
		"no scope":      {tenant.ErrNoScope, api.CodeTenantUnknown},
		"invalid scope": {tenant.ErrInvalidScope, api.CodeTenantUnknown},
		"wrapped":       {errors.Join(errors.New("context"), repo.ErrNotFound), api.CodeNotFound},
		"unrecognized":  {errors.New("something unexpected"), api.CodeInternal},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := api.FromError(tc.err)
			if got.Code != tc.wantCode {
				t.Errorf("FromError(%v).Code = %q, want %q", tc.err, got.Code, tc.wantCode)
			}
		})
	}
}

// An APIError must survive translation unchanged, so a handler that chose a
// specific code keeps it.
func TestFromErrorPreservesAPIError(t *testing.T) {
	t.Parallel()

	original := api.Errorf(api.CodeForbidden, "nope")

	if got := api.FromError(original); got != original {
		t.Errorf("FromError rewrote an APIError: %v", got)
	}

	// Even when wrapped.
	wrapped := errors.Join(errors.New("outer"), original)
	if got := api.FromError(wrapped); got.Code != api.CodeForbidden {
		t.Errorf("FromError(wrapped).Code = %q, want %q", got.Code, api.CodeForbidden)
	}
}

// An internal error's cause must never reach the client: unexpected errors are
// the ones most likely to carry a connection string or a file path.
func TestInternalErrorDoesNotLeakCause(t *testing.T) {
	t.Parallel()

	secret := "postgres://user:hunter2@db:5432/pivot"

	apiErr := api.FromError(errors.New("dial failed: " + secret))

	if strings.Contains(apiErr.Message, secret) {
		t.Errorf("the client-facing message leaks the cause: %q", apiErr.Message)
	}

	if apiErr.Message != api.CodeInternal.Summary() {
		t.Errorf("message = %q, want the generic summary", apiErr.Message)
	}

	// The cause must still be available for logging.
	if apiErr.Err == nil {
		t.Error("the cause was discarded; it is needed for the log")
	}
}
