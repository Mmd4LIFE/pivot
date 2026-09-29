package cli_test

import (
	"strings"
	"testing"
)

/*
`pivot admin limits`.

Part 22-a's Done-when is that the limits are visible in the product rather than
only in a config file, and this command is where "the product" is until Part 26
builds the screens. An untested version of it is the failure mode Part 19-d
already hit once: the command whose entire purpose is making something visible
had no test, so it could have shown nothing and looked fine.

Two halves, and the second is the one that matters. That it prints is easy.
That it prints what is actually in force -- rather than the defaults, or a
second copy of them that drifted -- is the claim.
*/
func TestLimitsShowsTheDefaults(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, map[string]string{}, "admin", "limits")
	if err != nil {
		t.Fatalf("limits: %v", err)
	}

	for _, want := range []string{
		"Queries per user, per connection",
		"PIVOT_QUERY_MAX_PER_USER",
		"Result cache",
		"PIVOT_QUERY_CACHE_MAX_ENTRY_BYTES",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the output does not mention %q:\n%s", want, stdout)
		}
	}

	// A zero timeout means "each connection decides", and printing it as "0s"
	// would read as "no time at all" -- the opposite.
	if strings.Contains(stdout, "\t0s") || strings.Contains(stdout, " 0s ") {
		t.Errorf("an unset ceiling printed as 0s:\n%s", stdout)
	}
}

// What it prints is what the operator set, not what the defaults are.
func TestLimitsReflectsWhatWasConfigured(t *testing.T) {
	t.Parallel()

	stdout, _, err := run(t, map[string]string{
		"PIVOT_QUERY_MAX_PER_USER":          "7",
		"PIVOT_QUERY_MAX_PER_CONNECTION":    "23",
		"PIVOT_QUERY_QUEUE_WAIT":            "12s",
		"PIVOT_QUERY_TIMEOUT":               "45s",
		"PIVOT_QUERY_CACHE_ENABLED":         "false",
		"PIVOT_QUERY_CACHE_MAX_BYTES":       "1048576",
		"PIVOT_QUERY_CACHE_MAX_ENTRY_BYTES": "524288",
	}, "admin", "limits")
	if err != nil {
		t.Fatalf("limits: %v", err)
	}

	for _, want := range []string{"7", "23", "12s", "45s", "off", "1 MiB", "512 KiB"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the output does not show the configured %q:\n%s", want, stdout)
		}
	}

	// And it does not still show the defaults it replaced.
	for _, gone := range []string{"64 MiB", "8 MiB"} {
		if strings.Contains(stdout, gone) {
			t.Errorf("the output still shows the default %q despite configuration:\n%s", gone, stdout)
		}
	}
}

/*
The per-connection limits are visible too.

`pivot admin limits` reads the resolved configuration, and the row cap, query
timeout and pool size live in the database instead — so without this the
instance-wide limits looked accounted for while the ones a query most often
meets were invisible. Between the two commands every limit has somewhere to be
seen.
*/
func TestListConnectionsShowsTheLimits(t *testing.T) {
	t.Parallel()

	_, envVars := withOrg(t)
	envVars["PIVOT_CONNECTION_PASSWORD"] = warehousePassword

	if _, _, err := addWarehouse(t, envVars); err != nil {
		t.Fatalf("add-connection: %v", err)
	}

	stdout, _, err := run(t, envVars, "admin", "list-connections", "--limits")
	if err != nil {
		t.Fatalf("list-connections --limits: %v", err)
	}

	for _, want := range []string{"MAX ROWS", "QUERY TIMEOUT", "POOL SIZE"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the output has no %q column:\n%s", want, stdout)
		}
	}

	// A zero is rendered as the default it means. Printing "0" would read as
	// "no rows allowed", which is the opposite of what it does.
	if !strings.Contains(stdout, "default") {
		t.Errorf("an unset limit did not print as a default:\n%s", stdout)
	}
}
