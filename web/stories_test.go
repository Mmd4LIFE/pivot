package web_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Coverage of the design system, checked from Go.
//
// The accessibility suite in src/ui/a11y.test.tsx scans every story it is
// given, which makes its import list the thing that decides what is checked. A
// component added without stories, or with stories nobody wired in, would pass
// by being absent — the failure mode of every hand-maintained list.
//
// This is the guard, and it is in Go on purpose: it runs in `make test` on a
// machine with no node_modules, which is where a missing story is most likely
// to go unnoticed.

const (
	uiDir    = "src/ui"
	suiteTSX = "src/ui/a11y.test.tsx"
)

// storyImport matches `import * as Button from "./Button.stories";`.
var storyImport = regexp.MustCompile(`import \* as (\w+) from "\./(\w+)\.stories"`)

// Every component has a story file, and every story file is in the suite.
func TestEveryComponentHasStories(t *testing.T) {
	t.Parallel()

	components, stories := componentsAndStories(t)

	if len(components) == 0 {
		t.Fatalf("found no components in %s; the layout has changed", uiDir)
	}

	missing := difference(components, stories)
	if len(missing) > 0 {
		t.Errorf("these components have no .stories.tsx: %s",
			strings.Join(missing, ", "))
	}

	orphaned := difference(stories, components)
	if len(orphaned) > 0 {
		t.Errorf("these story files have no component: %s",
			strings.Join(orphaned, ", "))
	}
}

// The accessibility suite imports every story file.
//
// Separate from the test above because the two fail for different reasons and
// are fixed in different files, and a single assertion covering both would say
// neither clearly.
func TestAccessibilitySuiteImportsEveryStory(t *testing.T) {
	t.Parallel()

	_, stories := componentsAndStories(t)

	data, err := os.ReadFile(suiteTSX)
	if err != nil {
		t.Fatalf("read %s: %v", suiteTSX, err)
	}

	imported := map[string]bool{}

	for _, m := range storyImport.FindAllStringSubmatch(string(data), -1) {
		// The alias and the file have to agree, or the STORY_MODULES shorthand
		// registers the module under a name that is not the component's.
		if m[1] != m[2] {
			t.Errorf("%s imports ./%s.stories as %s; the names must match",
				suiteTSX, m[2], m[1])
		}

		imported[m[2]] = true
	}

	for _, name := range stories {
		if !imported[name] {
			t.Errorf("%s does not import ./%s.stories, so its stories are never scanned",
				suiteTSX, name)
		}
	}

	// And the shorthand map lists each one. An import with no entry in
	// STORY_MODULES compiles and scans nothing.
	body := string(data)

	for name := range imported {
		if !regexp.MustCompile(`(?m)^\s+` + name + `,$`).MatchString(body) {
			t.Errorf("%s imports %s but does not list it in STORY_MODULES", suiteTSX, name)
		}
	}
}

// componentsAndStories returns the component and story base names in src/ui.
func componentsAndStories(t *testing.T) (components, stories []string) {
	t.Helper()

	entries, err := os.ReadDir(uiDir)
	if err != nil {
		t.Fatalf("read %s: %v", uiDir, err)
	}

	for _, entry := range entries {
		name := entry.Name()

		if entry.IsDir() || filepath.Ext(name) != ".tsx" {
			continue
		}

		switch {
		case strings.HasSuffix(name, ".stories.tsx"):
			stories = append(stories, strings.TrimSuffix(name, ".stories.tsx"))

		case strings.HasSuffix(name, ".test.tsx"):
			// The suite itself.

		default:
			components = append(components, strings.TrimSuffix(name, ".tsx"))
		}
	}

	sort.Strings(components)
	sort.Strings(stories)

	return components, stories
}

// difference returns the names in a that are not in b.
func difference(a, b []string) []string {
	have := make(map[string]bool, len(b))

	for _, name := range b {
		have[name] = true
	}

	var out []string

	for _, name := range a {
		if !have[name] {
			out = append(out, name)
		}
	}

	return out
}

// Nothing removes the focus indicator without replacing it.
//
// `outline-none` is a legitimate thing to write: a menu row shows focus with a
// background rather than a ring, and an outline drawn around a borderless
// input inside a bordered row looks like a rendering fault. What is not
// legitimate is writing it and stopping there, which leaves a keyboard user
// with no way to tell where they are — the single most common accessibility
// regression in an application like this one, and the reason the base
// stylesheet sets a `:focus-visible` outline for everything by default.
//
// The check is per file rather than per class list, which is coarse: a file
// that legitimately suppresses the ring in one place could hide an illegitimate
// suppression in another. It is still worth having, because the failure it
// guards against is somebody deleting a ring and never thinking about it again,
// and it fails loudly enough to make the next person say why.
func TestNothingRemovesTheFocusIndicator(t *testing.T) {
	t.Parallel()

	// What counts as putting one back.
	substitutes := []string{
		"data-[highlighted]", // a menu or listbox row
		"data-[selected",     // a command palette row
		"focus-visible:",     // an explicit ring
		"focus-within:",      // a ring on the container instead
	}

	entries, err := os.ReadDir(uiDir)
	if err != nil {
		t.Fatalf("read %s: %v", uiDir, err)
	}

	for _, entry := range entries {
		name := entry.Name()

		if entry.IsDir() || !strings.HasSuffix(name, ".tsx") {
			continue
		}

		if strings.Contains(name, ".stories.") || strings.Contains(name, ".test.") {
			continue
		}

		data, rerr := os.ReadFile(filepath.Join(uiDir, name))
		if rerr != nil {
			t.Fatalf("read %s: %v", name, rerr)
		}

		body := string(data)

		if !strings.Contains(body, "outline-none") {
			continue
		}

		replaced := false

		for _, substitute := range substitutes {
			if strings.Contains(body, substitute) {
				replaced = true

				break
			}
		}

		if !replaced {
			t.Errorf("%s removes the focus outline but names no replacement (one of %s)",
				name, strings.Join(substitutes, ", "))
		}
	}
}
