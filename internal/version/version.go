// Package version exposes build information stamped in at link time.
//
// Values are set by the Makefile (and later by the release pipeline) via
// -ldflags -X. They are deliberately unexported so that nothing can mutate
// them at runtime; read them through [Get].
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Stamped at build time via -ldflags. Defaults describe an unstamped build.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "unknown"
)

// Info describes the running build.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	BuiltBy   string `json:"builtBy"`
	GoVersion string `json:"goVersion"`
	Platform  string `json:"platform"`
}

// Get returns the build information for the running binary.
//
// When the binary was built without -ldflags stamping (for example by
// `go run`), the commit is recovered from the embedded VCS metadata that the
// Go toolchain adds automatically, so `go run ./cmd/pivot` still reports
// something useful.
func Get() Info {
	i := Info{
		Version:   version,
		Commit:    commit,
		Date:      date,
		BuiltBy:   builtBy,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}

	if i.Commit == "none" {
		if rev, ok := vcsRevision(); ok {
			i.Commit = rev
		}
	}

	return i
}

// String returns a single-line human-readable summary.
func (i Info) String() string {
	return fmt.Sprintf("pivot %s (commit %s, built %s by %s, %s, %s)",
		i.Version, i.Commit, i.Date, i.BuiltBy, i.GoVersion, i.Platform)
}

// Short returns just the version, for `--version`-style output.
func (i Info) Short() string { return i.Version }

// vcsRevision reads the commit hash the Go toolchain embeds in the binary.
func vcsRevision() (string, bool) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}

	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" {
			const shortLen = 7
			if len(s.Value) > shortLen {
				return s.Value[:shortLen], true
			}
			return s.Value, true
		}
	}

	return "", false
}
