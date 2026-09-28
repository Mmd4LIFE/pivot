//go:build !duckdb

package connectors_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
)

/*
What a default build says when somebody asks for DuckDB.

ADR-0010 keeps DuckDB out of the shipped binary, which makes this the error a
real person will actually meet -- probably after reading a blog post about
DuckDB federation and wondering why it does not work.

"No connector for \"duckdb\"" would be true and useless: it reads as though the
connector does not exist, when it exists and was left out of this binary on
purpose. So the test is on what the message teaches.
*/
func TestAskingADefaultBuildForDuckDBExplainsItself(t *testing.T) {
	t.Parallel()

	_, err := connectors.Open(connectors.Config{
		Kind: connectors.KindDuckDB, Database: "/data/warehouse.duckdb",
	})

	if err == nil {
		t.Fatal("a default build opened a DuckDB connection")
	}

	message := err.Error()

	for _, want := range []string{
		// That it is this build, rather than the product.
		"compiled without",
		// Where the decision is written down.
		"ADR-0010",
		// What to do instead, both ways.
		"make build-duckdb",
		"sqlite",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("the error does not mention %q:\n%s", want, message)
		}
	}
}

/*
And it is not offered where connectors are listed.

`--kind` and the registry advertise what can be opened. Listing a connector
this binary cannot open would turn one clear failure at configuration time into
a confusing one later, which is the opposite of what the message above is for.
*/
func TestADefaultBuildDoesNotOfferDuckDB(t *testing.T) {
	t.Parallel()

	kinds := connectors.Kinds()

	if slices.Contains(kinds, connectors.KindDuckDB) {
		t.Errorf("a build without DuckDB lists it as available: %v", kinds)
	}

	// And the three that are really there still are, so this is not passing
	// because the registry is empty.
	for _, want := range []connectors.Kind{
		connectors.KindPostgres, connectors.KindMySQL, connectors.KindSQLite,
	} {
		if !slices.Contains(kinds, want) {
			t.Errorf("%s is missing from %v", want, kinds)
		}
	}
}
