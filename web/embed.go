// Package web embeds the built browser application and serves it.
//
// This is what makes ADR-0001's single-binary promise hold with ADR-0002's
// React frontend: Vite builds static assets, they are compiled into the
// binary, and the Go process serves them. There is no Node at runtime and
// nothing to deploy alongside.
//
// The assets are committed as a placeholder rather than a build artifact, so
// `go build ./...` and the whole Go test suite work on a machine with no Node
// installed. A backend-only contributor never has to run the frontend build,
// and CI can order the two independently. [Built] reports which case it is.
package web

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/Mmd4LIFE/pivot/internal/logging"
)

// dist holds the built application.
//
// The `all:` prefix is required: without it, embed skips files beginning with
// a dot or underscore, and Vite emits neither — but the .gitkeep that keeps
// the directory in git is exactly such a file, and without it the embed fails
// on a fresh clone.
//
//go:embed all:dist
var dist embed.FS

// indexPath is the SPA's entry document.
const indexPath = "dist/index.html"

// Built reports whether a real frontend build is embedded.
//
// False on a clone where `make web-build` has not run. Every caller treats
// that as "serve an explanation", never as a fatal error: a backend developer
// running the server to exercise an API endpoint should not be stopped by a
// missing bundle.
func Built() bool {
	_, err := dist.ReadFile(indexPath)

	return err == nil
}

// Assets returns the embedded file tree rooted at the build output.
func Assets() (fs.FS, error) { return fs.Sub(dist, "dist") }

// Handler serves the embedded application.
//
// It is mounted as the catch-all outside the API prefix, so everything that is
// not an API call or a health probe arrives here.
func Handler() http.Handler {
	assets, err := Assets()
	if err != nil {
		return http.HandlerFunc(explainMissingBuild)
	}

	return HandlerFor(assets)
}

// HandlerFor serves an arbitrary asset tree.
//
// Separate from [Handler] so the routing and caching rules can be tested
// against a synthetic file system. Without it these tests would only run on a
// machine that had already built the frontend — which is to say, they would
// not run in the one place it matters most, a clean checkout.
func HandlerFor(assets fs.FS) http.Handler {
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		return http.HandlerFunc(explainMissingBuild)
	}

	files := http.FileServer(http.FS(assets))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The application is a document, not an API response, so it needs a
		// policy that permits its own scripts and styles. The router's default
		// is `default-src 'none'`, which is right for JSON and would render a
		// blank page here.
		applyDocumentPolicy(w)

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			// A POST to a client route is a mistake, and answering it with the
			// app shell would make that mistake look like a success.
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

			return
		}

		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")

		if name == "" || name == "index.html" {
			serveIndex(w, r, index)

			return
		}

		if file, ferr := assets.Open(name); ferr == nil {
			_ = file.Close()

			setAssetCaching(w, name)
			files.ServeHTTP(w, r)

			return
		}

		// Nothing on disk. A path that looks like a file is a 404, and a path
		// that looks like a route gets the app shell.
		//
		// The distinction matters: answering /assets/main-a1b2c3.js with HTML
		// produces a MIME-type error in the console rather than a missing-file
		// one, and that is a genuinely confusing hour for whoever debugs a bad
		// deploy.
		if path.Ext(name) != "" {
			http.NotFound(w, r)

			return
		}

		serveIndex(w, r, index)
	})
}

// serveIndex writes the application shell.
func serveIndex(w http.ResponseWriter, r *http.Request, index []byte) {
	// Never cached. index.html names the hashed bundles, so a stale copy
	// points at files that no longer exist — the classic white screen after a
	// deploy, fixed only by a hard refresh the user does not know to do.
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)

		return
	}

	writeBody(w, index)
}

// writeBody sends a response body.
//
// A failure here can only be recorded: the status line is already on the wire,
// so there is no error to return to anyone. It is logged at debug rather than
// error because the overwhelmingly common cause is a client that navigated
// away mid-response, and an access log full of those drowns the real ones.
func writeBody(w http.ResponseWriter, body []byte) {
	if _, err := w.Write(body); err != nil {
		slog.Default().Debug("could not write response body", logging.Err(err))
	}
}

// setAssetCaching applies the right lifetime for a static file.
func setAssetCaching(w http.ResponseWriter, name string) {
	// Vite emits content-hashed names under assets/, so the contents behind a
	// given URL can never change. A year and `immutable` is therefore safe,
	// and it is what keeps a repeat visit from revalidating every chunk.
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")

		return
	}

	// Everything else — favicons, manifests — is not hashed, so it gets a
	// short life instead.
	w.Header().Set("Cache-Control", "public, max-age=3600")
}

// DocumentCSP is the content security policy for the application shell.
//
// Deliberately different from the API's `default-src 'none'`. Each directive
// here earns its place:
//
//	script-src 'self'         no inline scripts, no CDN; an injected <script>
//	                          with a foreign src is refused
//	style-src adds 'unsafe-inline'
//	                          React and many component libraries set inline
//	                          styles. Refusing them would break layout, and a
//	                          style injection is a far smaller problem than a
//	                          script one
//	connect-src 'self'        the API is same-origin; exfiltration to another
//	                          host is refused
//	frame-ancestors 'none'    Pivot is not embeddable yet. Phase 8's embedding
//	                          relaxes this per-customer, deliberately
//	base-uri 'self'           an injected <base> would otherwise repoint every
//	                          relative URL on the page
//	form-action 'self'        a form cannot be made to post credentials
//	                          somewhere else
const DocumentCSP = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; " +
	"font-src 'self' data:; " +
	"connect-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"

// applyDocumentPolicy replaces the API's policy with the document one.
func applyDocumentPolicy(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", DocumentCSP)
}

// explainMissingBuild answers when no frontend has been built.
//
// A plain, accurate message rather than a 404. Somebody who has just cloned
// the repository and run the server should be told what to do, not left
// guessing why the page is empty.
func explainMissingBuild(w http.ResponseWriter, r *http.Request) {
	applyDocumentPolicy(w)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusServiceUnavailable)

	if r.Method == http.MethodHead {
		return
	}

	writeBody(w, []byte(
		"The Pivot frontend has not been built into this binary.\n\n"+
			"Run:\n\n"+
			"    make web-build && make build\n\n"+
			"The API is unaffected and is serving normally under /api/v1.\n"))
}
