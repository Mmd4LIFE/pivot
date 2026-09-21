package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/api"
)

func testRouter(t *testing.T, opts ...func(*api.RouterConfig)) http.Handler {
	t.Helper()

	cfg := api.RouterConfig{Log: discardLogger(), CORS: api.DefaultCORS()}
	for _, opt := range opts {
		opt(&cfg)
	}

	return api.NewRouter(cfg).Handler()
}

// do issues a request against a handler and returns the recorder.
func do(t *testing.T, h http.Handler, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(context.Background(), method, path, http.NoBody)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

// decodeError reads the standard error envelope.
func decodeErrorBody(t *testing.T, rec *httptest.ResponseRecorder) api.ErrorBody {
	t.Helper()

	var body api.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not the error envelope: %v\nbody: %s", err, rec.Body.String())
	}

	return body.Error
}

// An unknown API path must produce the structured envelope, not net/http's
// plain-text 404 — a client cannot branch on prose.
func TestUnknownAPIPathReturnsStructuredError(t *testing.T) {
	t.Parallel()

	rec := do(t, testRouter(t), http.MethodGet, api.APIPrefix+"/nonexistent", nil)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want JSON", ct)
	}

	body := decodeErrorBody(t, rec)

	if body.Code != api.CodeNotFound {
		t.Errorf("code = %q, want %q", body.Code, api.CodeNotFound)
	}

	if body.RequestID == "" {
		t.Error("the error carries no request ID")
	}

	if body.Docs == "" {
		t.Error("the error carries no docs link")
	}
}

// A panicking handler must become a 500 with a code, and the server must stay
// up — every other in-flight request depends on it.
func TestPanicBecomesCodedErrorAndServerSurvives(t *testing.T) {
	t.Parallel()

	router := api.NewRouter(api.RouterConfig{Log: discardLogger(), CORS: api.DefaultCORS()})
	router.Mux().HandleFunc("GET /boom", func(http.ResponseWriter, *http.Request) {
		panic("deliberate")
	})

	h := router.Handler()

	rec := do(t, h, http.MethodGet, "/boom", nil)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}

	body := decodeErrorBody(t, rec)
	if body.Code != api.CodeInternal {
		t.Errorf("code = %q, want %q", body.Code, api.CodeInternal)
	}

	// The panic value must not reach the client.
	if strings.Contains(rec.Body.String(), "deliberate") {
		t.Errorf("the panic value leaked to the client: %s", rec.Body.String())
	}

	// The handler still serves afterward, which is the point of recovering.
	if next := do(t, h, http.MethodGet, "/healthz", nil); next.Code != http.StatusOK {
		t.Errorf("a later request failed with %d; the panic was not contained", next.Code)
	}
}

// Rate limiting must return 429 with Retry-After, so a client knows when to
// come back rather than spinning.
func TestRateLimitReturns429WithRetryAfter(t *testing.T) {
	t.Parallel()

	h := testRouter(t, func(c *api.RouterConfig) {
		c.DefaultLimit = api.RateLimit{Rate: 1, Burst: 3}
	})

	var limited *httptest.ResponseRecorder

	for range 100 {
		rec := do(t, h, http.MethodGet, api.APIPrefix+"/anything", nil)
		if rec.Code == http.StatusTooManyRequests {
			limited = rec

			break
		}
	}

	if limited == nil {
		t.Fatal("100 rapid requests were never throttled")
	}

	body := decodeErrorBody(t, limited)
	if body.Code != api.CodeRateLimited {
		t.Errorf("code = %q, want %q", body.Code, api.CodeRateLimited)
	}

	retry := limited.Header().Get("Retry-After")
	if retry == "" {
		t.Fatal("a throttled response carries no Retry-After")
	}

	secs, err := strconv.Atoi(retry)
	if err != nil || secs < 1 {
		t.Errorf("Retry-After = %q, want a positive integer", retry)
	}
}

// Health probes must not be rate limited: throttling a readiness probe makes
// an orchestrator kill a healthy instance exactly when it is busiest.
func TestHealthProbesAreNotRateLimited(t *testing.T) {
	t.Parallel()

	h := testRouter(t, func(c *api.RouterConfig) {
		c.DefaultLimit = api.RateLimit{Rate: 1, Burst: 1}
	})

	for i := range 50 {
		if rec := do(t, h, http.MethodGet, "/healthz", nil); rec.Code != http.StatusOK {
			t.Fatalf("probe %d was throttled with %d", i, rec.Code)
		}
	}
}

func TestSecurityHeadersArePresent(t *testing.T) {
	t.Parallel()

	rec := do(t, testRouter(t), http.MethodGet, "/healthz", nil)

	want := map[string]string{
		"X-Content-Type-Options":       "nosniff",
		"X-Frame-Options":              "DENY",
		"Referrer-Policy":              "no-referrer",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
	}

	for header, expected := range want {
		if got := rec.Header().Get(header); got != expected {
			t.Errorf("%s = %q, want %q", header, got, expected)
		}
	}

	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("CSP = %q, want frame-ancestors 'none'", csp)
	}

	// HSTS is meaningless over plaintext and would pin a browser to https for
	// a host that does not serve it.
	if hsts := rec.Header().Get("Strict-Transport-Security"); hsts != "" {
		t.Errorf("HSTS = %q on a plaintext request, want it absent", hsts)
	}
}

func TestRequestIDIsGeneratedAndEchoed(t *testing.T) {
	t.Parallel()

	h := testRouter(t)

	t.Run("generated when absent", func(t *testing.T) {
		rec := do(t, h, http.MethodGet, "/healthz", nil)
		if rec.Header().Get(api.RequestIDHeader) == "" {
			t.Error("no request ID was generated")
		}
	})

	t.Run("client value is honored", func(t *testing.T) {
		rec := do(t, h, http.MethodGet, "/healthz",
			map[string]string{api.RequestIDHeader: "abc-123"})

		if got := rec.Header().Get(api.RequestIDHeader); got != "abc-123" {
			t.Errorf("request ID = %q, want the client's value", got)
		}
	})

	// An unvalidated client value lands in every log line for the request,
	// which is a log-injection vector, not merely untidy.
	t.Run("hostile value is sanitized", func(t *testing.T) {
		rec := do(t, h, http.MethodGet, "/healthz",
			map[string]string{api.RequestIDHeader: `evil"} {"level":"FATAL`})

		got := rec.Header().Get(api.RequestIDHeader)
		for _, bad := range []string{`"`, `{`, `}`, ` `, `:`} {
			if strings.Contains(got, bad) {
				t.Errorf("request ID %q still contains %q", got, bad)
			}
		}
	})

	t.Run("overlong value is truncated", func(t *testing.T) {
		rec := do(t, h, http.MethodGet, "/healthz",
			map[string]string{api.RequestIDHeader: strings.Repeat("a", 5000)})

		if got := rec.Header().Get(api.RequestIDHeader); len(got) > 128 {
			t.Errorf("request ID length = %d, want it bounded", len(got))
		}
	})
}

// CORS defaults to closed. A wildcard with credentials would let any website
// make authenticated requests, so it must be refused rather than downgraded.
func TestCORS(t *testing.T) {
	t.Parallel()

	t.Run("closed by default", func(t *testing.T) {
		rec := do(t, testRouter(t), http.MethodGet, "/healthz",
			map[string]string{"Origin": "https://evil.example"})

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Allow-Origin = %q on a closed config, want empty", got)
		}
	})

	t.Run("allowed origin is echoed with Vary", func(t *testing.T) {
		h := testRouter(t, func(c *api.RouterConfig) {
			c.CORS.AllowedOrigins = []string{"https://app.example"}
		})

		rec := do(t, h, http.MethodGet, "/healthz",
			map[string]string{"Origin": "https://app.example"})

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
			t.Errorf("Allow-Origin = %q, want the origin echoed", got)
		}

		if !strings.Contains(rec.Header().Get("Vary"), "Origin") {
			t.Error("Vary does not include Origin; a shared cache could cross origins")
		}
	})

	t.Run("wildcard with credentials is refused", func(t *testing.T) {
		h := testRouter(t, func(c *api.RouterConfig) {
			c.CORS.AllowedOrigins = []string{"*"}
			c.CORS.AllowCredentials = true
		})

		rec := do(t, h, http.MethodGet, "/healthz",
			map[string]string{"Origin": "https://evil.example"})

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Allow-Origin = %q; a wildcard with credentials must be refused", got)
		}
	})

	t.Run("preflight is answered", func(t *testing.T) {
		h := testRouter(t, func(c *api.RouterConfig) {
			c.CORS.AllowedOrigins = []string{"https://app.example"}
		})

		rec := do(t, h, http.MethodOptions, "/healthz",
			map[string]string{"Origin": "https://app.example"})

		if rec.Code != http.StatusNoContent {
			t.Errorf("preflight status = %d, want 204", rec.Code)
		}

		if rec.Header().Get("Access-Control-Allow-Methods") == "" {
			t.Error("preflight did not advertise allowed methods")
		}
	})
}
