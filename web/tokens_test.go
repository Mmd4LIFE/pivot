package web_test

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Contrast, checked against the design tokens rather than against a rendering.
//
// An accessibility scan in a browser checks whatever happened to be on screen:
// it catches the component someone wrote a story for and misses the one they
// did not. The tokens are where the answer actually lives, so checking them
// covers every component that will ever use them, including the ones not
// written yet.
//
// It also needs no browser, so it runs in `make test` on a machine with no
// Node installed — which is the same reason the rest of web/'s tests use a
// synthetic asset tree.
//
// This complements a real axe scan rather than replacing it: axe catches
// structure (labels, roles, focus order) that a color pair cannot express, and
// a browser catches the case where a component ignores the tokens and
// hard-codes a color. Both are wanted; this is the half that can run anywhere.

const tokensFile = "src/styles/tokens.css"

// WCAG 2.1 thresholds.
const (
	// aaText is the ratio required for body text: 1.4.3 Contrast (Minimum).
	aaText = 4.5

	// aaLarge applies to text at 18.66px bold or 24px regular, and — via 1.4.11
	// Non-text Contrast — to the visual boundary of a control and to anything
	// that has to be *seen* rather than read.
	aaLarge = 3.0
)

// rgb is a parsed token color.
type rgb struct{ r, g, b float64 }

// luminance is WCAG's relative luminance.
//
// Not a simple average: the coefficients weight green far above blue because
// the eye does, which is why a mid blue on white fails a contrast check that a
// mid green of the same "brightness" passes.
func (c rgb) luminance() float64 {
	channel := func(v float64) float64 {
		v /= 255

		if v <= 0.03928 {
			return v / 12.92
		}

		return math.Pow((v+0.055)/1.055, 2.4)
	}

	return 0.2126*channel(c.r) + 0.7152*channel(c.g) + 0.0722*channel(c.b)
}

// contrast returns the WCAG contrast ratio between two colors, 1 to 21.
func contrast(a, b rgb) float64 {
	la, lb := a.luminance(), b.luminance()

	if la < lb {
		la, lb = lb, la
	}

	return (la + 0.05) / (lb + 0.05)
}

// themeTokens holds one theme's color tokens.
type themeTokens map[string]rgb

var (
	// The light theme is the :root block; the dark theme is the explicit
	// [data-theme="dark"] block. The media-query block is a copy of the latter
	// for browsers where the bootstrap script could not run, and is checked
	// separately for drift rather than parsed twice.
	lightBlock = regexp.MustCompile(`(?sm)^:root \{(.*?)\n\}`)
	darkBlock  = regexp.MustCompile(`(?s):root\[data-theme="dark"\] \{(.*?)\n\}`)
	// The fallback block is nested inside the media query, so its closing
	// brace is indented two spaces. That indentation is what stops `.*?` from
	// stopping at the inner block's own closing brace.
	mediaBlock = regexp.MustCompile(`(?s):root:not\(\[data-theme="light"\]\) \{(.*?)\n {2}\}`)

	tokenLine = regexp.MustCompile(`--pivot-([a-z-]+):\s*(\d+)\s+(\d+)\s+(\d+);`)
)

// parseTokens reads the color tokens out of a CSS block.
func parseTokens(block string) themeTokens {
	out := themeTokens{}

	for _, m := range tokenLine.FindAllStringSubmatch(block, -1) {
		r, _ := strconv.Atoi(m[2])
		g, _ := strconv.Atoi(m[3])
		b, _ := strconv.Atoi(m[4])

		out[m[1]] = rgb{float64(r), float64(g), float64(b)}
	}

	return out
}

// loadThemes reads both themes from the token file.
func loadThemes(t *testing.T) (light, dark themeTokens) {
	t.Helper()

	data, err := os.ReadFile(tokensFile)
	if err != nil {
		t.Fatalf("read %s: %v", tokensFile, err)
	}

	css := string(data)

	lm := lightBlock.FindStringSubmatch(css)
	if lm == nil {
		t.Fatalf("%s has no :root block", tokensFile)
	}

	dm := darkBlock.FindStringSubmatch(css)
	if dm == nil {
		t.Fatalf("%s has no [data-theme=dark] block", tokensFile)
	}

	light, dark = parseTokens(lm[1]), parseTokens(dm[1])

	if len(light) == 0 || len(dark) == 0 {
		t.Fatalf("parsed %d light and %d dark tokens; the format has changed",
			len(light), len(dark))
	}

	return light, dark
}

// pairing is one foreground/background combination that has to be legible.
type pairing struct {
	foreground string
	background string
	minimum    float64
	why        string
}

// pairings are the combinations the components actually produce.
//
// Only real ones: asserting every token against every other would fail on
// combinations nothing renders, and a test that has to be argued with is a
// test people delete.
var pairings = []pairing{
	// Body text, on each surface it can land on.
	{"text", "canvas", aaText, "body text on the page background"},
	{"text", "surface", aaText, "body text on a card"},
	{"text", "surface-raised", aaText, "body text in a popover or dialog"},
	{"text", "surface-sunken", aaText, "body text in an inset area"},

	// Secondary text is still text, so it is still 4.5:1. The temptation to
	// call it "large" is how secondary text ends up unreadable.
	{"text-muted", "canvas", aaText, "secondary text"},
	{"text-muted", "surface", aaText, "secondary text on a card"},

	// Inverted text appears on the accent fill, which is what a primary
	// button is.
	{"accent-text", "accent", aaText, "a primary button's label"},
	{"accent-text", "accent-hover", aaText, "a primary button's label while hovered"},
	{"text-inverted", "text", aaText, "a tooltip's text on its dark fill"},

	// Status colors used as text.
	{"danger", "surface", aaText, "an error message"},
	{"danger", "canvas", aaText, "an error message on the page background"},
	{"success", "surface", aaText, "a success message"},
	{"warning", "surface", aaText, "a warning message"},
	{"info", "surface", aaText, "an informational message"},

	// Accent as a link color, which is text.
	{"accent", "surface", aaText, "a link"},
	{"accent", "canvas", aaText, "a link on the page background"},

	// Non-text: 1.4.11 asks for 3:1 on anything that must be seen rather than
	// read. A border nobody can see is a control nobody can find.
	{"border-strong", "surface", aaLarge, "the visible edge of an input"},
	{"border-strong", "canvas", aaLarge, "the visible edge of a control"},

	// The focus ring is the single most important non-text contrast in the
	// system: it is what a keyboard user navigates by.
	{"accent", "canvas", aaLarge, "the focus ring against the page"},
	{"accent", "surface", aaLarge, "the focus ring against a card"},
}

// Every pairing must meet its threshold, in both themes.
func TestTokenContrast(t *testing.T) {
	t.Parallel()

	light, dark := loadThemes(t)

	for name, theme := range map[string]themeTokens{"light": light, "dark": dark} {
		t.Run(name, func(t *testing.T) {
			for _, p := range pairings {
				fg, ok := theme[p.foreground]
				if !ok {
					t.Errorf("the %s theme has no --pivot-%s", name, p.foreground)

					continue
				}

				bg, ok := theme[p.background]
				if !ok {
					t.Errorf("the %s theme has no --pivot-%s", name, p.background)

					continue
				}

				ratio := contrast(fg, bg)
				if ratio < p.minimum {
					t.Errorf("%s on %s is %s, want at least %.1f:1 — %s",
						p.foreground, p.background, ratioString(ratio), p.minimum, p.why)
				}
			}
		})
	}
}

func ratioString(r float64) string {
	return fmt.Sprintf("%.2f:1", r)
}

// Both themes must define the same tokens.
//
// A token present in one theme and missing from the other renders with the
// other theme's value still applied — so a dark-mode user sees a light-mode
// color, which is the kind of bug that only shows up in a screenshot.
func TestThemesDefineTheSameTokens(t *testing.T) {
	t.Parallel()

	light, dark := loadThemes(t)

	// The dark block overrides colors only; shape and type tokens are declared
	// once in :root and inherited. So the check is one-directional: everything
	// dark defines must exist in light.
	for name := range dark {
		if _, ok := light[name]; !ok {
			t.Errorf("--pivot-%s is defined for dark but not for light", name)
		}
	}
}

// The media-query fallback must agree with the explicit dark theme.
//
// Two copies of the same values exist on purpose — the attribute selector wins
// when the bootstrap script ran, the media query covers the case where it could
// not — and two copies drift. A user with blocked storage would then get a
// different dark theme from everyone else, which nobody would ever notice.
func TestDarkFallbackMatchesTheExplicitTheme(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(tokensFile)
	if err != nil {
		t.Fatalf("read %s: %v", tokensFile, err)
	}

	css := string(data)

	mm := mediaBlock.FindStringSubmatch(css)
	if mm == nil {
		t.Fatalf("%s has no prefers-color-scheme fallback block", tokensFile)
	}

	dm := darkBlock.FindStringSubmatch(css)
	if dm == nil {
		t.Fatalf("%s has no [data-theme=dark] block", tokensFile)
	}

	fallback, explicit := parseTokens(mm[1]), parseTokens(dm[1])

	for name, want := range explicit {
		got, ok := fallback[name]
		if !ok {
			t.Errorf("--pivot-%s is in the dark theme but not in the media-query fallback", name)

			continue
		}

		if got != want {
			t.Errorf("--pivot-%s differs: fallback has %s, the dark theme has %s",
				name, format(got), format(want))
		}
	}

	for name := range fallback {
		if _, ok := explicit[name]; !ok {
			t.Errorf("--pivot-%s is in the media-query fallback but not in the dark theme", name)
		}
	}
}

func format(c rgb) string {
	return strings.TrimSpace(fmt.Sprintf("%d %d %d", int(c.r), int(c.g), int(c.b)))
}
