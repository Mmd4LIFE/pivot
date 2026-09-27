package connectors_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
)

/*
The connector interface, without a database.

What is tested here is the part every later connector inherits: the registry,
the capability declarations the query compiler will read, and the promise that
a configuration never carries its password into a log. The failures that need a
real PostgreSQL are in postgres_test.go.
*/

func TestTheRegistryKnowsWhatThisBuildHas(t *testing.T) {
	t.Parallel()

	kinds := connectors.Kinds()

	if len(kinds) == 0 {
		t.Fatal("no connectors are registered; the init in postgres.go did not run")
	}

	var found bool

	for _, k := range kinds {
		if k == connectors.KindPostgres {
			found = true
		}
	}

	if !found {
		t.Errorf("postgres is not registered: %v", kinds)
	}
}

// An unknown kind is refused by name, and the error says what this build does
// have -- because the answer to "unknown connector" is almost always a typo,
// and a list is what makes that obvious.
func TestAnUnknownKindIsRefusedWithTheAlternatives(t *testing.T) {
	t.Parallel()

	_, err := connectors.Open(connectors.Config{Kind: "oracle"})

	if err == nil {
		t.Fatal("an unknown connector kind was accepted")
	}

	if !strings.Contains(err.Error(), "oracle") {
		t.Errorf("error = %v, want it to name the kind", err)
	}

	if !strings.Contains(err.Error(), "postgres") {
		t.Errorf("error = %v, want it to list what is available", err)
	}
}

/*
A config does not carry its password into a log.

Redacted is a method rather than a convention because "remember not to log the
config" holds right up until somebody is debugging at two in the morning.
*/
func TestARedactedConfigHasNoPassword(t *testing.T) {
	t.Parallel()

	cfg := connectors.Config{
		Kind: connectors.KindPostgres, Host: "db.internal", Database: "warehouse",
		Username: "analytics", Password: "the-actual-password",
	}

	redacted := cfg.Redacted()

	if redacted.Password != "" {
		t.Error("Redacted kept the password")
	}

	// Everything else survives, or a redacted config is useless for the
	// diagnosis it exists to serve.
	if redacted.Host != cfg.Host || redacted.Username != cfg.Username ||
		redacted.Database != cfg.Database {
		t.Errorf("Redacted lost something that was not secret: %+v", redacted)
	}

	// And the original is untouched: a value receiver, so a caller that
	// redacts for a log still has a usable config afterwards.
	if cfg.Password == "" {
		t.Error("Redacted mutated the config it was called on")
	}
}

// Capabilities are declared, and the ones the query compiler will read have to
// be right rather than plausible.
func TestPostgresDeclaresItsDialect(t *testing.T) {
	t.Parallel()

	c := mustOpen(t, connectors.Config{
		Kind: connectors.KindPostgres, Host: "localhost",
		Database: "x", Username: "y",
	})

	defer func() { _ = c.Close() }()

	caps := c.Capabilities()

	if !caps.WindowFunctions || !caps.CTEs || !caps.LateralJoins {
		t.Errorf("postgres should declare window functions, CTEs and lateral joins: %+v", caps)
	}

	if caps.Placeholder != connectors.PlaceholderDollar {
		t.Errorf("placeholder = %q, want dollar", caps.Placeholder)
	}

	// 63, and it truncates silently rather than erroring -- so two generated
	// aliases differing after the 63rd character become one column.
	if caps.MaxIdentifierLength != 63 {
		t.Errorf("MaxIdentifierLength = %d, want 63", caps.MaxIdentifierLength)
	}

	if !caps.SupportsCancel {
		t.Error("postgres cancellation reaches the server; the capability says otherwise")
	}
}

/*
Quoting an identifier is the whole of the injection risk.

An identifier that can end its own quoting can start a statement, so a name
containing a double quote has to come back with it doubled rather than escaped
some other way or rejected.
*/
func TestIdentifierQuotingClosesTheInjection(t *testing.T) {
	t.Parallel()

	c := mustOpen(t, connectors.Config{
		Kind: connectors.KindPostgres, Host: "localhost", Database: "x", Username: "y",
	})

	defer func() { _ = c.Close() }()

	quote := c.Capabilities().QuoteIdentifier

	for in, want := range map[string]string{
		"users":               `"users"`,
		"Mixed Case":          `"Mixed Case"`,
		`we"ird`:              `"we""ird"`,
		`"; DROP TABLE x; --`: `"""; DROP TABLE x; --"`,
	} {
		if got := quote(in); got != want {
			t.Errorf("QuoteIdentifier(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestPlaceholdersAreRenderedPerDialect(t *testing.T) {
	t.Parallel()

	if got := connectors.PlaceholderDollar.Format(3); got != "$3" {
		t.Errorf("dollar placeholder 3 = %q, want $3", got)
	}

	if got := connectors.PlaceholderQuestion.Format(3); got != "?" {
		t.Errorf("question placeholder 3 = %q, want ?", got)
	}
}

/*
A configuration that cannot work is refused before anything dials.

"a PostgreSQL connection needs host, database" is a sentence somebody can act
on. A dial error against an empty hostname is not.
*/
func TestAnIncompleteConfigIsRefusedBeforeDialing(t *testing.T) {
	t.Parallel()

	for name, cfg := range map[string]connectors.Config{
		"no host":     {Kind: connectors.KindPostgres, Database: "x", Username: "y"},
		"no database": {Kind: connectors.KindPostgres, Host: "h", Username: "y"},
		"no username": {Kind: connectors.KindPostgres, Host: "h", Database: "x"},
		"impossible port": {
			Kind: connectors.KindPostgres, Host: "h", Database: "x",
			Username: "y", Port: 70000,
		},
		"unknown sslmode": {
			Kind: connectors.KindPostgres, Host: "h", Database: "x",
			Username: "y", SSLMode: "sort-of",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := connectors.Open(cfg); err == nil {
				t.Errorf("%s was accepted", name)
			}
		})
	}
}

// A connector error can be matched on its reason alone, which is what lets a
// caller branch on "the credentials were refused" without parsing prose.
func TestAnErrorMatchesOnItsReason(t *testing.T) {
	t.Parallel()

	err := connectors.Errorf(connectors.ReasonAuth, errors.New("28P01"),
		"check the password", "refused")

	if !errors.Is(err, &connectors.Error{Reason: connectors.ReasonAuth}) {
		t.Error("an auth error does not match ReasonAuth")
	}

	if errors.Is(err, &connectors.Error{Reason: connectors.ReasonTimeout}) {
		t.Error("an auth error matches ReasonTimeout")
	}

	// The hint reaches the reader, and the driver's original text stays
	// available for the log without being in the message.
	if !strings.Contains(err.Error(), "check the password") {
		t.Errorf("Error() = %q, want the hint in it", err.Error())
	}

	if strings.Contains(err.Error(), "28P01") {
		t.Errorf("Error() = %q, want the driver's text kept out of it", err.Error())
	}

	if !strings.Contains(errors.Unwrap(err).Error(), "28P01") {
		t.Error("the driver's original error was not kept")
	}
}

func mustOpen(t *testing.T, cfg connectors.Config) connectors.Connector {
	t.Helper()

	c, err := connectors.Open(cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	return c
}
