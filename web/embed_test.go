package web_test

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Mmd4LIFE/pivot/web"
)

// These run against a synthetic asset tree rather than the embedded one, so
// they pass on a clean checkout where no frontend has been built. That is
// deliberate: the rules below matter most to somebody who has just cloned the
// repository, and a test that only runs after `make web-build` would not
// protect them.

// testAssets stands in for a Vite build: a shell, a hashed bundle, and an
// unhashed file at the root.
func testAssets() fs.FS {
	return fstest.MapFS{
		"index.html":             {Data: []byte("<!doctype html><div id=root></div>")},
		"assets/main-a1b2c3.js":  {Data: []byte("console.log('pivot')")},
		"assets/main-a1b2c3.css": {Data: []byte(":root{--pivot-accent:37 99 235}")},
		"favicon.ico":            {Data: []byte("icon")},
	}
}

func do(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(
		context.Background(), method, target, http.NoBody))

	return rec
}

// A client route that exists only in the browser must receive the shell.
//
// This is the whole point of an SPA fallback: the router lives in JavaScript,
// so the server cannot know which paths are real and must hand over the app
// and let it decide.
func TestClientRoutesReceiveTheShell(t *testing.T) {
	t.Parallel()

	h := web.HandlerFor(testAssets())

	for _, route := range []string{"/", "/dashboards", "/dashboards/7", "/settings/users"} {
		rec := do(t, h, http.MethodGet, route)

		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", route, rec.Code)
		}

		if !strings.Contains(rec.Body.String(), "id=root") {
			t.Errorf("GET %s did not return the app shell: %s", route, rec.Body.String())
		}

		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("GET %s Content-Type = %q, want HTML", route, ct)
		}
	}
}

// A missing file must be a 404, never the shell.
//
// Serving HTML for /assets/main-deadbeef.js produces a MIME-type error in the
// browser console rather than a missing-file one, and that turns a bad deploy
// into a genuinely confusing hour.
func TestMissingAssetsAre404AndNotTheShell(t *testing.T) {
	t.Parallel()

	h := web.HandlerFor(testAssets())

	for _, missing := range []string{
		"/assets/main-deadbeef.js",
		"/assets/styles.css",
		"/manifest.json",
		"/logo.png",
	} {
		rec := do(t, h, http.MethodGet, missing)

		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", missing, rec.Code)
		}

		if strings.Contains(rec.Body.String(), "id=root") {
			t.Errorf("GET %s returned the app shell instead of 404", missing)
		}
	}
}

// Real assets are served as themselves.
func TestAssetsAreServed(t *testing.T) {
	t.Parallel()

	h := web.HandlerFor(testAssets())

	rec := do(t, h, http.MethodGet, "/assets/main-a1b2c3.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	if !strings.Contains(rec.Body.String(), "console.log") {
		t.Errorf("body = %q, want the asset's contents", rec.Body.String())
	}
}

// Caching: hashed assets forever, the shell never.
//
// The shell names the hashed bundles, so a cached copy points at files that no
// longer exist after a deploy — the white screen that only a hard refresh
// fixes, which a user does not know to do.
func TestCachingPolicy(t *testing.T) {
	t.Parallel()

	h := web.HandlerFor(testAssets())

	t.Run("hashed assets are immutable", func(t *testing.T) {
		rec := do(t, h, http.MethodGet, "/assets/main-a1b2c3.js")

		cache := rec.Header().Get("Cache-Control")
		if !strings.Contains(cache, "immutable") || !strings.Contains(cache, "max-age=31536000") {
			t.Errorf("Cache-Control = %q, want a year and immutable", cache)
		}
	})

	t.Run("the shell is never cached", func(t *testing.T) {
		for _, route := range []string{"/", "/dashboards"} {
			rec := do(t, h, http.MethodGet, route)

			if cache := rec.Header().Get("Cache-Control"); !strings.Contains(cache, "no-cache") {
				t.Errorf("GET %s Cache-Control = %q, want no-cache", route, cache)
			}
		}
	})

	t.Run("unhashed files get a short life", func(t *testing.T) {
		rec := do(t, h, http.MethodGet, "/favicon.ico")

		cache := rec.Header().Get("Cache-Control")
		if strings.Contains(cache, "immutable") {
			t.Errorf("Cache-Control = %q; an unhashed file must not be immutable", cache)
		}
	})
}

// The document policy must permit the application's own scripts.
//
// The API's policy is `default-src 'none'`, which is right for JSON and would
// render a blank page here. Part 5 left this explicitly to Part 9.
func TestDocumentPolicyAllowsTheApplication(t *testing.T) {
	t.Parallel()

	h := web.HandlerFor(testAssets())
	rec := do(t, h, http.MethodGet, "/")

	csp := rec.Header().Get("Content-Security-Policy")

	if csp == "" {
		t.Fatal("the shell was served with no Content-Security-Policy")
	}

	if strings.Contains(csp, "default-src 'none'") {
		t.Error("the shell carries the API's policy; nothing would load")
	}

	for _, required := range []string{
		"script-src 'self'",
		"connect-src 'self'",
		"frame-ancestors 'none'",
		"base-uri 'self'",
		"object-src 'none'",
	} {
		if !strings.Contains(csp, required) {
			t.Errorf("policy is missing %q:\n%s", required, csp)
		}
	}

	// A CDN or an inline script would both defeat the point of having one.
	if strings.Contains(csp, "script-src") && strings.Contains(csp, "'unsafe-inline' 'unsafe-eval'") {
		t.Errorf("the script policy is too permissive:\n%s", csp)
	}
}

// A POST to a client route is a mistake, and answering with the shell would
// make it look like a success.
func TestNonGETIsRefused(t *testing.T) {
	t.Parallel()

	h := web.HandlerFor(testAssets())

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := do(t, h, method, "/dashboards")

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /dashboards = %d, want 405", method, rec.Code)
		}
	}
}

// HEAD answers with the headers and no body, which is what a health checker
// or a link checker expects.
func TestHeadReturnsNoBody(t *testing.T) {
	t.Parallel()

	h := web.HandlerFor(testAssets())

	rec := do(t, h, http.MethodHead, "/")
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	if rec.Body.Len() != 0 {
		t.Errorf("HEAD returned a body of %d bytes", rec.Body.Len())
	}
}

// A path traversal must not escape the asset tree.
func TestPathTraversalIsRefused(t *testing.T) {
	t.Parallel()

	h := web.HandlerFor(testAssets())

	for _, hostile := range []string{
		"/../embed.go",
		"/assets/../../embed.go",
		"/%2e%2e/embed.go",
	} {
		rec := do(t, h, http.MethodGet, hostile)

		if strings.Contains(rec.Body.String(), "package web") {
			t.Errorf("GET %s escaped the asset tree", hostile)
		}
	}
}

// With no build embedded, the server explains itself rather than 404ing.
//
// Somebody who has just cloned the repository and run the server should be
// told what to do, not left wondering why the page is blank.
func TestMissingBuildExplainsItself(t *testing.T) {
	t.Parallel()

	h := web.HandlerFor(fstest.MapFS{})

	rec := do(t, h, http.MethodGet, "/")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}

	if !strings.Contains(rec.Body.String(), "make web-build") {
		t.Errorf("the message does not say how to fix it:\n%s", rec.Body.String())
	}
}

// The shell must contain no inline script.
//
// It is served with `script-src 'self'`, which blocks them. An inline script
// therefore does nothing at all — no console error a build would catch, no
// failed request, just a feature that quietly is not there. That is exactly
// how the theme bootstrap shipped broken here until the compiled HTML was read
// by hand, so it is checked rather than remembered.
func TestShellHasNoInlineScript(t *testing.T) {
	t.Parallel()

	if !web.Built() {
		t.Skip("no frontend build embedded; run `make web-build`")
	}

	assets, err := web.Assets()
	if err != nil {
		t.Fatalf("assets: %v", err)
	}

	shell, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}

	for _, script := range inlineScripts(string(shell)) {
		t.Errorf("index.html has an inline script the CSP will block:\n%s", script)
	}
}

// inlineScripts returns the bodies of any <script> elements with content.
func inlineScripts(html string) []string {
	var found []string

	rest := html

	for {
		open := strings.Index(rest, "<script")
		if open < 0 {
			return found
		}

		gt := strings.Index(rest[open:], ">")
		if gt < 0 {
			return found
		}

		bodyStart := open + gt + 1

		end := strings.Index(rest[bodyStart:], "</script>")
		if end < 0 {
			return found
		}

		if body := strings.TrimSpace(rest[bodyStart : bodyStart+end]); body != "" {
			found = append(found, body)
		}

		rest = rest[bodyStart+end:]
	}
}

// The embedded tree must at least be embeddable. This is what catches a
// deleted placeholder, which would break `go build` on a fresh clone.
func TestEmbeddedTreeIsReadable(t *testing.T) {
	t.Parallel()

	if _, err := web.Assets(); err != nil {
		t.Fatalf("the embedded asset tree is unreadable: %v", err)
	}
}

// When a frontend *has* been built, the token indirection must have survived.
//
// This is the Phase 8 requirement made checkable: if Tailwind resolved the
// theme at build time, the CSS would contain literal colors and a runtime
// override of --pivot-* would do nothing. Skipped when nothing is built.
func TestBuiltCSSKeepsTheTokenIndirection(t *testing.T) {
	t.Parallel()

	if !web.Built() {
		t.Skip("no frontend build embedded; run `make web-build`")
	}

	assets, err := web.Assets()
	if err != nil {
		t.Fatalf("assets: %v", err)
	}

	var checked int

	err = fs.WalkDir(assets, ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || !strings.HasSuffix(p, ".css") {
			return walkErr
		}

		data, rerr := fs.ReadFile(assets, p)
		if rerr != nil {
			return rerr
		}

		checked++

		css := string(data)

		if !strings.Contains(css, "var(--pivot-") {
			t.Errorf("%s contains no var(--pivot-*): the theme was resolved at "+
				"build time, so a runtime override cannot work", p)
		}

		// `<alpha-value>` is Tailwind 3 syntax. Tailwind 4 emits it verbatim,
		// producing `rgb(var(--x) / <alpha-value>)` — invalid CSS that every
		// browser silently drops, so every utility using it does nothing at
		// all and nothing reports an error. It went unnoticed here until the
		// compiled output was read by hand.
		//
		// Checked by name rather than by looking for angle brackets: Tailwind
		// 4's own `@property` rules legitimately contain `syntax:"<length>"`
		// and friends, so the broader check is a false positive. (It was, for
		// about a minute.)
		if strings.Contains(css, "<alpha-value>") {
			t.Errorf("%s contains the Tailwind 3 <alpha-value> placeholder; the "+
				"browser will discard those declarations and the styling will "+
				"silently do nothing", p)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if checked == 0 {
		t.Error("the build contains no CSS at all")
	}
}
