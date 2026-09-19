package version

import (
	"runtime"
	"strings"
	"testing"
)

func TestGetPopulatesRuntimeFields(t *testing.T) {
	t.Parallel()

	got := Get()

	if got.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %q, want %q", got.GoVersion, runtime.Version())
	}

	wantPlatform := runtime.GOOS + "/" + runtime.GOARCH
	if got.Platform != wantPlatform {
		t.Errorf("Platform = %q, want %q", got.Platform, wantPlatform)
	}

	if got.Version == "" {
		t.Error("Version is empty; it should always have a value")
	}
}

func TestInfoStringIncludesEveryField(t *testing.T) {
	t.Parallel()

	i := Info{
		Version:   "1.2.3",
		Commit:    "abc1234",
		Date:      "2026-09-19T00:00:00Z",
		BuiltBy:   "ci",
		GoVersion: "go1.27.1",
		Platform:  "linux/amd64",
	}

	got := i.String()

	for _, want := range []string{
		"1.2.3", "abc1234", "2026-09-19T00:00:00Z", "ci", "go1.27.1", "linux/amd64",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, missing %q", got, want)
		}
	}
}

func TestInfoShortReturnsOnlyVersion(t *testing.T) {
	t.Parallel()

	i := Info{Version: "1.2.3", Commit: "abc1234"}

	if got := i.Short(); got != "1.2.3" {
		t.Errorf("Short() = %q, want %q", got, "1.2.3")
	}
}

// The default ldflags-less build must still report a usable commit, because
// `go run ./cmd/pivot` is the common development path and "none" is unhelpful
// in a bug report.
func TestGetRecoversCommitFromBuildInfo(t *testing.T) {
	t.Parallel()

	got := Get()

	if got.Commit == "" {
		t.Error("Commit is empty; expected either a stamped value or the VCS revision")
	}
}
