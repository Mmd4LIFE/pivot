package query

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

/*
The single door.

ADR-0009 makes the semantic compiler the only place row-level security is
injected. That guarantee is worth exactly nothing if a handler can open a
connector itself and send whatever it likes, and "please go through the
pipeline" is a code review convention that holds until the afternoon somebody
is in a hurry. These two tests are what make it a property.

They read the source rather than the build graph on purpose. Once the HTTP
layer has a query endpoint it will depend on this package, and this package
depends on connectors, so every transitive check is satisfied by construction
and proves nothing. What can be proved is that no file in the HTTP layer names
the connector package, and that the set of packages which do is a list
somebody chose.
*/

const connectorsPkg = "github.com/Mmd4LIFE/pivot/internal/connectors"

/*
allowedToOpenAConnector is every package that may reach a source directly, and
why. Adding to it is a deliberate act, which is the entire mechanism.

Nothing here runs a user's query. The catalog reads a schema, the CLI proves a
connection works, and both are Pivot asking a question of its own -- neither
carries a statement anybody typed. A package that wants to run *somebody's*
SQL belongs behind [Executor], and if this list grows one that does, the
reason for the list is gone.
*/
var allowedToOpenAConnector = map[string]string{
	"internal/connectors/conformance": "" +
		"is the suite every connector is measured against",
	"internal/query":   "is the pipeline: the door itself",
	"internal/catalog": "introspects schemas; it does not run anybody's query",
	"internal/cli":     "tests a connection and triggers a sync, on an operator's behalf",
}

// No file under internal/api may name the connector package. An HTTP handler
// reaches a source through [Executor] or not at all.
func TestNoHandlerCanReachAConnector(t *testing.T) {
	t.Parallel()

	offenders := importersOf(t, connectorsPkg)

	for _, pkg := range offenders {
		if strings.HasPrefix(pkg, "internal/api") {
			t.Errorf("%s imports the connector package; it must go through query.Executor", pkg)
		}
	}
}

/*
And the wider rule: every package that opens a connector is on the list above.

A failure here is not necessarily a bug. It is a new package reaching a
source, and the question it asks is whether that package should be running
through the pipeline instead. Answer it, then add the line.
*/
func TestOnlyDeclaredPackagesOpenAConnector(t *testing.T) {
	t.Parallel()

	for _, pkg := range importersOf(t, connectorsPkg) {
		if _, ok := allowedToOpenAConnector[pkg]; !ok {
			t.Errorf("%s imports the connector package and is not in "+
				"allowedToOpenAConnector; should it go through query.Executor?", pkg)
		}
	}

	// The list must not outlive what it describes, or it stops being a list
	// somebody maintains and becomes one nobody reads.
	importing := importersOf(t, connectorsPkg)

	for pkg := range allowedToOpenAConnector {
		if !slices.Contains(importing, pkg) {
			t.Errorf("allowedToOpenAConnector lists %s, which no longer imports "+
				"the connector package; remove the line", pkg)
		}
	}
}

/*
importersOf returns every package in the repository whose non-test files
import the given path, as a module-relative directory.

Non-test files only. A test may open a connector to build a fixture -- the
catalog's own tests run against a real SQLite source -- and a test is not a
path a request can take.
*/
func importersOf(t *testing.T, target string) []string {
	t.Helper()

	root := moduleRoot(t)
	fset := token.NewFileSet()

	var found []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if name := d.Name(); name != "." && (strings.HasPrefix(name, ".") ||
				name == "bin" || name == "vendor" || name == "node_modules") {
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}

		for _, spec := range file.Imports {
			imported, uerr := strconv.Unquote(spec.Path.Value)
			if uerr != nil {
				return uerr
			}

			if imported != target {
				continue
			}

			pkg, rerr := filepath.Rel(root, filepath.Dir(path))
			if rerr != nil {
				return rerr
			}

			if !slices.Contains(found, pkg) {
				found = append(found, pkg)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk the repository: %v", err)
	}

	slices.Sort(found)

	return found
}

// moduleRoot finds the directory holding go.mod, walking up from the test's
// working directory.
func moduleRoot(t *testing.T) string {
	t.Helper()

	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}

	for {
		if _, serr := filepath.Glob(filepath.Join(dir, "go.mod")); serr == nil {
			if matches, _ := filepath.Glob(filepath.Join(dir, "go.mod")); len(matches) == 1 {
				return dir
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's working directory")
		}

		dir = parent
	}
}
