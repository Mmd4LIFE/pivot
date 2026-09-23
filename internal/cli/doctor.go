package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/Mmd4LIFE/pivot/internal/config"
	"github.com/Mmd4LIFE/pivot/internal/logging"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/version"
	"github.com/Mmd4LIFE/pivot/web"
)

/*
`pivot doctor`.

The command exists for the moment somebody's install does not work and they
have no idea why. Everything it checks is something that has actually gone
wrong for somebody: a config file that is not where they think it is, a
database directory that is not writable, a schema older than the binary, a port
already taken, a frontend that was never built into the binary.

Each check reports one of three outcomes, and the distinction is the point:

	ok    this is fine
	warn  this works but will bite you
	fail  this is why it does not work

A warning never sets the exit status. An operator who runs `pivot doctor` in a
provisioning script needs it to fail only on things that are actually broken,
or they will stop running it.
*/

// severity is a check's outcome.
type severity int

const (
	severityOK severity = iota
	severityWarn
	severityFail
)

func (s severity) label() string {
	switch s {
	case severityOK:
		return "ok"
	case severityWarn:
		return "warn"
	case severityFail:
		return "FAIL"
	default:
		return "?"
	}
}

// finding is one check's result.
type finding struct {
	name     string
	severity severity
	detail   string

	// hint is what to do about it. Only on warnings and failures, because a
	// check that passed has nothing to suggest.
	hint string
}

func ok(name, detail string) finding {
	return finding{name: name, severity: severityOK, detail: detail}
}

func warn(name, detail, hint string) finding {
	return finding{name: name, severity: severityWarn, detail: detail, hint: hint}
}

func fail(name, detail, hint string) finding {
	return finding{name: name, severity: severityFail, detail: detail, hint: hint}
}

// newDoctorCmd builds `pivot doctor`.
func newDoctorCmd(env Env, flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose an installation",
		Long: `Check an installation and report what is wrong with it.

Runs through configuration, the database, the schema, the listener and the
embedded frontend, and says which of them is the problem. Exits non-zero if
any check failed, so it can be used in a provisioning script; warnings do not
affect the exit status.

Safe to run against a live instance: everything here reads.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			findings := diagnose(cmd.Context(), env, cmd, flags)

			return report(env.Stdout, findings)
		},
	}
}

// diagnose runs every check and returns what it found.
//
// The checks are ordered so that the first failure is the most likely cause
// rather than a symptom: configuration before database, database before
// schema, schema before anything that depends on it. A failure early on makes
// the later checks meaningless, and they say so rather than adding noise.
func diagnose(ctx context.Context, env Env, cmd *cobra.Command, flags *globalFlags) []finding {
	findings := []finding{versionCheck()}

	res, err := loadConfig(cmd, env, flags)
	if err != nil {
		return append(findings, fail("configuration",
			"could not be loaded: "+err.Error(),
			"run `pivot config show` to see where it is looking"))
	}

	findings = append(findings,
		configCheck(res),
		frontendCheck(),
		listenerCheck(ctx, res.Config.Server),
	)

	findings = append(findings, databaseFindings(ctx, env, res.Config)...)

	return findings
}

func versionCheck() finding {
	info := version.Get()

	return ok("version", fmt.Sprintf("%s (%s, built %s)", info.Version, info.Commit, info.Date))
}

// configCheck reports where the configuration came from.
//
// "Which file is it actually reading?" is the first question of nearly every
// configuration problem, and the answer is usually "not the one you edited".
func configCheck(res *config.Result) finding {
	if res.SourceFile == "" {
		return ok("configuration", "no file; defaults and environment only")
	}

	return ok("configuration", "loaded from "+res.SourceFile)
}

// frontendCheck reports whether this binary can serve the application.
//
// A warning rather than a failure: an API-only deployment is legitimate. But
// somebody who opens the browser and gets a 404 needs to be told that the
// binary they built has no frontend in it, which `make build` without
// `make web-build` produces.
func frontendCheck() finding {
	if web.Built() {
		return ok("frontend", "embedded")
	}

	return warn("frontend", "not embedded in this binary",
		"the API works; the browser application will 404. Build with `make all`")
}

// listenerCheck reports whether the configured address can be bound.
//
// It binds and immediately closes, which is the only way to find out. A port
// already in use is the single most common reason `pivot serve` exits
// straight after being started by a service manager, and the error it prints
// scrolls past.
func listenerCheck(ctx context.Context, cfg config.ServerConfig) finding {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))

	var lc net.ListenConfig

	listener, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fail("listener", addr+" cannot be bound: "+err.Error(),
			"another process is using it, or the port is privileged")
	}

	_ = listener.Close()

	// Deliberately not "the port is free": between this check and the server
	// starting, anything could take it. What was established is that it was
	// bindable a moment ago, which is what the operator needs to know.
	return ok("listener", addr+" was bindable")
}

// databaseFindings covers everything that needs the database open.
//
// Grouped, because each one depends on the last: there is no point reporting
// on the schema of a database that could not be opened, and a list of five
// failures with one cause is harder to read than one failure.
func databaseFindings(ctx context.Context, env Env, cfg *config.Config) []finding {
	engine, _, err := store.ParseURL(cfg.Database.URL)
	if err != nil {
		return []finding{fail("database url", err.Error(),
			"expected a SQLite path, sqlite://, :memory:, or postgres://")}
	}

	findings := []finding{ok("database url", string(engine))}

	// The file checks come first and run even if the database cannot be
	// opened, because "the directory is not writable" explains "could not
	// open" and the reverse is not true.
	if path, isFile := store.SQLitePath(cfg.Database.URL); isFile {
		findings = append(findings, sqliteFileChecks(path)...)
	}

	log := logging.New(config.LogConfig{Level: "error", Format: "text"}, io.Discard)

	db, err := store.Open(ctx, cfg.Database, log)
	if err != nil {
		return append(findings, fail("database", "could not be opened: "+err.Error(),
			"check the URL, the credentials, and that the server is reachable"))
	}

	defer func() { _ = db.Close() }()

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if herr := db.HealthCheck(pingCtx); herr != nil {
		return append(findings, fail("database", "opened but does not answer: "+herr.Error(),
			"the server may be starting, overloaded, or behind a firewall that accepts and drops"))
	}

	findings = append(findings, ok("database", "reachable"))

	return append(findings, schemaCheck(ctx, db))
}

// sqliteFileChecks covers the two things that go wrong with a file database.
//
// Both are about the *directory*, not the file: SQLite writes a journal and a
// WAL beside the database, so a writable file in a read-only directory fails
// on the first write with a message about the database being read-only, which
// sends people to look at the wrong thing.
func sqliteFileChecks(path string) []finding {
	dir := filepath.Dir(path)

	findings := make([]finding, 0, 2)

	info, err := os.Stat(path)

	switch {
	case err == nil:
		findings = append(findings, ok("database file",
			fmt.Sprintf("%s (%d bytes)", path, info.Size())))

		if info.Mode().Perm()&0o077 != 0 {
			findings = append(findings, warn("database permissions",
				fmt.Sprintf("%s is mode %o", path, info.Mode().Perm()),
				"it holds session tokens and stored secrets; 0600 is the right mode"))
		}

	case errors.Is(err, os.ErrNotExist):
		// Not a failure. A first run has no database yet, and creating it is
		// what the first start does.
		findings = append(findings, ok("database file",
			path+" does not exist yet; it will be created"))

	default:
		findings = append(findings, fail("database file", err.Error(),
			"check the path and its permissions"))
	}

	if werr := writable(dir); werr != nil {
		findings = append(findings, fail("database directory",
			dir+" is not writable: "+werr.Error(),
			"SQLite writes a WAL and a journal beside the database, so the "+
				"directory must be writable even when the file exists"))
	} else {
		findings = append(findings, ok("database directory", dir+" is writable"))
	}

	return findings
}

// writable reports whether a directory can be written to, by writing to it.
//
// Reading the mode bits is not the same question: a directory can be mode 0777
// and still refuse a write because of the filesystem being read-only, a full
// disk, SELinux, or a container's mount options. All four have happened.
func writable(dir string) error {
	f, err := os.CreateTemp(dir, ".pivot-doctor-*")
	if err != nil {
		return err
	}

	name := f.Name()

	_ = f.Close()
	_ = os.Remove(name)

	return nil
}

// schemaCheck compares the schema in the database with the one this binary
// expects.
//
// A binary running against an older schema is the failure mode of a deployment
// where the migration step was skipped, and its symptom is an error from deep
// inside a query about a column that does not exist.
func schemaCheck(ctx context.Context, db *store.DB) finding {
	statuses, err := store.Status(ctx, db)
	if err != nil {
		return fail("schema", "could not be read: "+err.Error(),
			"run `pivot migrate status`")
	}

	var pending int

	for _, s := range statuses {
		if !s.Applied {
			pending++
		}
	}

	current, err := store.CurrentVersion(ctx, db)
	if err != nil {
		return fail("schema", "version could not be read: "+err.Error(),
			"run `pivot migrate status`")
	}

	if pending == 0 {
		return ok("schema", fmt.Sprintf("version %d, up to date", current))
	}

	return fail("schema",
		fmt.Sprintf("version %d, with %d migration(s) pending", current, pending),
		"run `pivot migrate up`, or set database.autoMigrate")
}

// report prints the findings and returns an error if any of them failed.
//
// The error is what sets the exit status. Its text is deliberately short: the
// detail is already on screen, and repeating it at the end makes the last
// thing somebody reads a duplicate of the first.
func report(w io.Writer, findings []finding) error {
	var failures int

	fmt.Fprintln(w)

	for _, f := range findings {
		fmt.Fprintf(w, "  %-5s %-22s %s\n", f.severity.label(), f.name, f.detail)

		if f.hint != "" {
			fmt.Fprintf(w, "        %-22s %s\n", "", "-> "+f.hint)
		}

		if f.severity == severityFail {
			failures++
		}
	}

	fmt.Fprintln(w)

	if failures == 0 {
		fmt.Fprintln(w, "  No problems found.")
		fmt.Fprintln(w)

		return nil
	}

	return fmt.Errorf("%d check(s) failed", failures)
}
