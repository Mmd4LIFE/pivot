package cli_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

/*
The commands an operator actually runs, driven end to end.

Every one of these executes the real command tree against a real SQLite file
in t.TempDir(). Nothing is stubbed: `migrate up` runs the embedded migrations,
`admin create-user` hashes a password with Argon2 and writes a row, and
`grant-role` writes a Zanzibar tuple. That is the point — these commands are
the first thing anybody touches on a new install, and a test that mocked the
database would not have caught any of the failures they can actually have.

They are slower than a unit test for the same reason, and worth it.
*/

// instance returns the environment for a throwaway SQLite install.
//
// A file rather than :memory:, because each command opens its own connection
// and an in-memory database would vanish between them — which is exactly the
// arrangement a real install has.
func instance(t *testing.T) map[string]string {
	t.Helper()

	return map[string]string{
		"PIVOT_DATABASE_URL": "sqlite://" + filepath.Join(t.TempDir(), "pivot.db"),
		"PIVOT_LOG_LEVEL":    "error",
	}
}

// migrated returns the environment for an instance with the schema applied.
func migrated(t *testing.T) map[string]string {
	t.Helper()

	env := instance(t)

	if _, _, err := run(t, env, "migrate", "up"); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	return env
}

// withPassword adds the non-interactive password source.
func withPassword(env map[string]string, password string) map[string]string {
	env["PIVOT_ADMIN_PASSWORD"] = password

	return env
}

const password = "a-sufficiently-long-password"

// ── migrate ──────────────────────────────────────────────────────────────────

func TestMigrateUpFromScratch(t *testing.T) {
	t.Parallel()

	env := instance(t)

	stdout, _, err := run(t, env, "migrate", "up")
	if err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	if !strings.Contains(stdout, "Migrated") {
		t.Errorf("stdout = %q, want it to report the migration", stdout)
	}
}

func TestMigrateUpIsIdempotent(t *testing.T) {
	t.Parallel()

	env := migrated(t)

	// The second run must be a no-op rather than an error. An operator who
	// runs it twice, or a container that runs it on every start, has done
	// nothing wrong.
	stdout, _, err := run(t, env, "migrate", "up")
	if err != nil {
		t.Fatalf("second migrate up: %v", err)
	}

	if !strings.Contains(strings.ToLower(stdout), "already") &&
		!strings.Contains(stdout, "up to date") {
		t.Errorf("stdout = %q, want it to say there was nothing to do", stdout)
	}
}

func TestMigrateStatusAndVersion(t *testing.T) {
	t.Parallel()

	env := migrated(t)

	status, _, err := run(t, env, "migrate", "status")
	if err != nil {
		t.Fatalf("migrate status: %v", err)
	}

	if !strings.Contains(status, "00001") {
		t.Errorf("status = %q, want it to list the applied migrations", status)
	}

	versionOut, _, err := run(t, env, "migrate", "version")
	if err != nil {
		t.Fatalf("migrate version: %v", err)
	}

	if strings.TrimSpace(versionOut) == "" {
		t.Error("migrate version printed nothing")
	}
}

func TestMigrateVersionOnAnUnmigratedDatabase(t *testing.T) {
	t.Parallel()

	// Version zero is an answer, not a failure: a fresh database has simply
	// not been migrated yet, and an operator checking before migrating should
	// not get an error.
	stdout, _, err := run(t, instance(t), "migrate", "version")
	if err != nil {
		t.Fatalf("migrate version on a fresh database: %v", err)
	}

	if !strings.Contains(stdout, "0") {
		t.Errorf("stdout = %q, want it to report version 0", stdout)
	}
}

func TestMigrateCreateWritesBothDialects(t *testing.T) {
	dir := t.TempDir()

	// Not parallel: the command writes into the migrations directory relative
	// to the working directory, so it needs one to itself.
	restore := chdir(t, dir)
	defer restore()

	for _, dialect := range []string{"postgres", "sqlite"} {
		if err := os.MkdirAll(filepath.Join(dir, "internal", "store", "migrations", dialect), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	if _, _, err := run(t, instance(t), "migrate", "create", "add widgets"); err != nil {
		t.Fatalf("migrate create: %v", err)
	}

	// Both engines, always. A migration written for one and not the other is
	// the single way ADR-0003's dual-engine promise breaks quietly.
	for _, dialect := range []string{"postgres", "sqlite"} {
		matches, _ := filepath.Glob(filepath.Join(dir, "internal", "store", "migrations", dialect, "*add_widgets.sql"))

		if len(matches) == 0 {
			t.Errorf("no migration created for %s", dialect)
		}
	}
}

func TestMigrateCreateRejectsAnEmptyName(t *testing.T) {
	t.Parallel()

	if _, _, err := run(t, instance(t), "migrate", "create", "   "); err == nil {
		t.Error("migrate create accepted a blank name")
	}
}

// ── admin create-user ────────────────────────────────────────────────────────

func TestCreateUserCreatesTheOrganizationAndGrantsAdmin(t *testing.T) {
	t.Parallel()

	env := withPassword(migrated(t), password)

	stdout, _, err := run(t, env,
		"admin", "create-user",
		"--create-org", "Acme Analytics",
		"--email", "ada@example.com",
		"--name", "Ada Lovelace",
	)
	if err != nil {
		t.Fatalf("create-user: %v", err)
	}

	// The slug is derived, and getting it wrong means the operator cannot name
	// the organization at the login prompt afterwards.
	if !strings.Contains(stdout, "acme-analytics") {
		t.Errorf("stdout = %q, want the derived slug", stdout)
	}

	// The first user in an organization becomes its admin, or a fresh install
	// has nobody who can ever grant anything.
	if !strings.Contains(stdout, "admin role") {
		t.Errorf("stdout = %q, want it to report the admin grant", stdout)
	}
}

func TestCreateUserRefusesADuplicateEmail(t *testing.T) {
	t.Parallel()

	env := withPassword(migrated(t), password)

	if _, _, err := run(t, env, "admin", "create-user",
		"--create-org", "Acme", "--email", "ada@example.com"); err != nil {
		t.Fatalf("first create-user: %v", err)
	}

	if _, _, err := run(t, env, "admin", "create-user",
		"--email", "ada@example.com"); err == nil {
		t.Error("create-user accepted a duplicate email address")
	}
}

func TestCreateUserRefusesAShortPassword(t *testing.T) {
	t.Parallel()

	env := withPassword(migrated(t), "short")

	_, _, err := run(t, env, "admin", "create-user",
		"--create-org", "Acme", "--email", "ada@example.com")
	if err == nil {
		t.Fatal("create-user accepted a password under the minimum length")
	}

	if !strings.Contains(err.Error(), "12") {
		t.Errorf("error = %q, want it to name the minimum length", err)
	}
}

func TestCreateUserNeedsAnOrganizationWhenSeveralExist(t *testing.T) {
	t.Parallel()

	env := withPassword(migrated(t), password)

	for _, org := range []string{"Acme", "Globex"} {
		if _, _, err := run(t, env, "admin", "create-user",
			"--create-org", org,
			"--email", strings.ToLower(org)+"@example.com"); err != nil {
			t.Fatalf("seeding %s: %v", org, err)
		}
	}

	// Ambiguous, so it must refuse rather than guess. Guessing would put the
	// user in whichever organization happened to be first.
	_, _, err := run(t, env, "admin", "create-user", "--email", "third@example.com")
	if err == nil {
		t.Fatal("create-user guessed an organization when two exist")
	}

	if !strings.Contains(err.Error(), "--org") {
		t.Errorf("error = %q, want it to name the flag that resolves this", err)
	}
}

func TestCreateUserAcceptsANamedOrganization(t *testing.T) {
	t.Parallel()

	env := withPassword(migrated(t), password)

	for _, org := range []string{"Acme", "Globex"} {
		if _, _, err := run(t, env, "admin", "create-user",
			"--create-org", org,
			"--email", strings.ToLower(org)+"@example.com"); err != nil {
			t.Fatalf("seeding %s: %v", org, err)
		}
	}

	if _, _, err := run(t, env, "admin", "create-user",
		"--org", "globex", "--email", "third@example.com"); err != nil {
		t.Fatalf("create-user --org: %v", err)
	}
}

func TestCreateUserRejectsAnUnknownOrganization(t *testing.T) {
	t.Parallel()

	env := withPassword(migrated(t), password)

	if _, _, err := run(t, env, "admin", "create-user",
		"--org", "nowhere", "--email", "ada@example.com"); err == nil {
		t.Error("create-user accepted an organization that does not exist")
	}
}

func TestCreateUserNeedsAPassword(t *testing.T) {
	t.Parallel()

	// No PIVOT_ADMIN_PASSWORD and no terminal to prompt from.
	_, _, err := run(t, migrated(t), "admin", "create-user",
		"--create-org", "Acme", "--email", "ada@example.com")
	if err == nil {
		t.Error("create-user succeeded with no password available")
	}
}

// ── admin reset-password ─────────────────────────────────────────────────────

func TestResetPassword(t *testing.T) {
	t.Parallel()

	env := withPassword(migrated(t), password)

	if _, _, err := run(t, env, "admin", "create-user",
		"--create-org", "Acme", "--email", "ada@example.com"); err != nil {
		t.Fatalf("create-user: %v", err)
	}

	env["PIVOT_ADMIN_PASSWORD"] = "an-entirely-different-password"

	stdout, _, err := run(t, env, "admin", "reset-password", "--email", "ada@example.com")
	if err != nil {
		t.Fatalf("reset-password: %v", err)
	}

	// Every session ends. A password reset that left them alive would leave a
	// thief signed in after the owner locked them out.
	if !strings.Contains(strings.ToLower(stdout), "session") {
		t.Errorf("stdout = %q, want it to report the revoked sessions", stdout)
	}
}

func TestResetPasswordRejectsAnUnknownUser(t *testing.T) {
	t.Parallel()

	env := withPassword(migrated(t), password)

	if _, _, err := run(t, env, "admin", "create-user",
		"--create-org", "Acme", "--email", "ada@example.com"); err != nil {
		t.Fatalf("create-user: %v", err)
	}

	if _, _, err := run(t, env, "admin", "reset-password",
		"--email", "nobody@example.com"); err == nil {
		t.Error("reset-password accepted an address with no account")
	}
}

// ── admin grant-role / revoke-role ───────────────────────────────────────────

func seedTwoUsers(t *testing.T) map[string]string {
	t.Helper()

	env := withPassword(migrated(t), password)

	for _, email := range []string{"ada@example.com", "grace@example.com"} {
		args := []string{"admin", "create-user", "--email", email}
		if email == "ada@example.com" {
			args = append(args, "--create-org", "Acme")
		}

		if _, _, err := run(t, env, args...); err != nil {
			t.Fatalf("seeding %s: %v", email, err)
		}
	}

	return env
}

func TestGrantAndRevokeRole(t *testing.T) {
	t.Parallel()

	env := seedTwoUsers(t)

	if _, _, err := run(t, env, "admin", "grant-role",
		"--email", "grace@example.com", "--role", "editor"); err != nil {
		t.Fatalf("grant-role: %v", err)
	}

	if _, _, err := run(t, env, "admin", "revoke-role",
		"--email", "grace@example.com", "--role", "editor"); err != nil {
		t.Fatalf("revoke-role: %v", err)
	}
}

func TestGrantRoleRejectsAnUnknownRole(t *testing.T) {
	t.Parallel()

	env := seedTwoUsers(t)

	_, _, err := run(t, env, "admin", "grant-role",
		"--email", "grace@example.com", "--role", "sorcerer")
	if err == nil {
		t.Fatal("grant-role accepted a role that does not exist")
	}

	// The message has to list the real ones, or the operator's next move is a
	// guess.
	if !strings.Contains(err.Error(), "admin") {
		t.Errorf("error = %q, want it to list the valid roles", err)
	}
}

func TestGrantRoleRejectsAnUnknownUser(t *testing.T) {
	t.Parallel()

	env := seedTwoUsers(t)

	if _, _, err := run(t, env, "admin", "grant-role",
		"--email", "nobody@example.com", "--role", "editor"); err == nil {
		t.Error("grant-role accepted an address with no account")
	}
}

func TestRevokingTheLastAdminIsRefusedOrWarned(t *testing.T) {
	t.Parallel()

	env := seedTwoUsers(t)

	// Ada is the only administrator. Removing her leaves an installation
	// nobody can administer, so the command must at minimum say so.
	stdout, stderr, err := run(t, env, "admin", "revoke-role",
		"--email", "ada@example.com", "--role", "admin")

	combined := strings.ToLower(stdout + stderr)
	if err == nil && !strings.Contains(combined, "last") && !strings.Contains(combined, "warn") {
		t.Errorf("revoking the last admin passed silently; stdout=%q stderr=%q", stdout, stderr)
	}
}

// ── admin add-provider ───────────────────────────────────────────────────────

func TestAddProvider(t *testing.T) {
	t.Parallel()

	env := withPassword(migrated(t), password)

	if _, _, err := run(t, env, "admin", "create-user",
		"--create-org", "Acme", "--email", "ada@example.com"); err != nil {
		t.Fatalf("create-user: %v", err)
	}

	stdout, _, err := run(t, env, "admin", "add-provider",
		"--slug", "okta",
		"--name", "Okta",
		"--issuer", "https://example.okta.com",
		"--client-id", "0oa1234567890",
	)
	if err != nil {
		t.Fatalf("add-provider: %v", err)
	}

	if !strings.Contains(stdout, "okta") {
		t.Errorf("stdout = %q, want it to name the provider", stdout)
	}
}

func TestAddProviderRequiresItsFlags(t *testing.T) {
	t.Parallel()

	env := withPassword(migrated(t), password)

	if _, _, err := run(t, env, "admin", "create-user",
		"--create-org", "Acme", "--email", "ada@example.com"); err != nil {
		t.Fatalf("create-user: %v", err)
	}

	// No issuer and no client id: the provider would be unusable, so it must
	// not be written at all.
	if _, _, err := run(t, env, "admin", "add-provider",
		"--slug", "okta", "--name", "Okta"); err == nil {
		t.Error("add-provider accepted a provider with no issuer")
	}
}

// ── healthcheck ──────────────────────────────────────────────────────────────

func TestHealthcheckAgainstAHealthyServer(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	stdout, _, err := run(t, nil, "healthcheck", "--url", server.URL+"/healthz")
	if err != nil {
		t.Fatalf("healthcheck: %v", err)
	}

	if !strings.Contains(stdout, "healthy") {
		t.Errorf("stdout = %q, want it to report health", stdout)
	}
}

func TestHealthcheckFailsOnANonOKStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	// The exit status is the entire interface a container HEALTHCHECK reads.
	_, _, err := run(t, nil, "healthcheck", "--url", server.URL+"/healthz")
	if err == nil {
		t.Fatal("healthcheck reported a 503 as healthy")
	}

	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error = %q, want it to name the status", err)
	}
}

func TestHealthcheckFailsWhenNothingIsListening(t *testing.T) {
	t.Parallel()

	// Port 1 is privileged and never bound by a test.
	if _, _, err := run(t, nil, "healthcheck", "--url", "http://127.0.0.1:1/healthz"); err == nil {
		t.Error("healthcheck reported an unreachable address as healthy")
	}
}

func TestHealthcheckHonorsItsTimeout(t *testing.T) {
	t.Parallel()

	block := make(chan struct{})
	defer close(block)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	defer server.Close()

	start := time.Now()

	_, _, err := run(t, nil, "healthcheck",
		"--url", server.URL+"/healthz", "--timeout", "150ms")
	if err == nil {
		t.Fatal("healthcheck waited out a server that never answered")
	}

	// A HEALTHCHECK that hangs is worse than one that fails: the orchestrator
	// learns nothing and the container sits in "starting" forever.
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("took %s, want it to give up after the timeout", elapsed)
	}
}

func TestHealthcheckDerivesTheAddressFromConfiguration(t *testing.T) {
	t.Parallel()

	// No --url: inside a container this is how it is invoked, so the address
	// has to come from the same configuration the server binds with.
	env := map[string]string{
		"PIVOT_SERVER_HOST": "",
		"PIVOT_SERVER_PORT": "1",
		"PIVOT_LOG_LEVEL":   "error",
	}

	_, _, err := run(t, env, "healthcheck", "--timeout", "500ms")
	if err == nil {
		t.Fatal("healthcheck found something listening on port 1")
	}

	// An empty host means "bind everything", which is not connectable. The
	// probe must have rewritten it to loopback.
	if !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Errorf("error = %q, want it to probe loopback", err)
	}
}

func TestHealthcheckReadyProbesReadiness(t *testing.T) {
	t.Parallel()

	var path string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if _, _, err := run(t, nil, "healthcheck", "--url", server.URL+"/readyz", "--ready"); err != nil {
		t.Fatalf("healthcheck --ready: %v", err)
	}

	if path != "/readyz" {
		t.Errorf("probed %q, want /readyz", path)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

// freePort asks the operating system for a port nobody is using.
//
// Port 0 would be simpler and the configuration rejects it, correctly: a
// server told to bind port 0 in production has been misconfigured, and the
// validator has no way to know a test meant it. So the port is resolved here
// instead, which also lets these tests run in parallel with each other.
func freePort(t *testing.T) int {
	t.Helper()

	var config net.ListenConfig

	listener, err := config.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port

	if err := listener.Close(); err != nil {
		t.Fatalf("releasing the port: %v", err)
	}

	return port
}

// chdir moves into dir and returns a function that moves back.
func chdir(t *testing.T, dir string) func() {
	t.Helper()

	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	return func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("chdir back: %v", err)
		}
	}
}

// ── serve ────────────────────────────────────────────────────────────────────

// serveUntilCanceled starts the server, waits until it is listening, stops it,
// and returns its output.
//
// Waiting for the listener rather than sleeping a fixed time is not tidiness.
// A fixed 400ms passed consistently and then failed under `-race`, where the
// migrations take longer: the context was canceled mid-migration and the
// command reported a partial migration rather than the thing being tested. A
// test whose result depends on how fast the machine is will eventually fail on
// somebody else's.
//
// Nothing connects to the server beyond the readiness poll -- the browser suite
// does that -- what is checked here is that the command starts, says what it is
// doing, and drains when asked.
func serveUntilCanceled(t *testing.T, env map[string]string) (string, string, error) {
	t.Helper()

	port := freePort(t)
	env["PIVOT_SERVER_PORT"] = strconv.Itoa(port)
	env["PIVOT_LOG_LEVEL"] = "info"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})

	var stdout, stderr string
	var err error

	go func() {
		defer close(done)
		stdout, stderr, err = runCtx(ctx, t, env, "serve")
	}()

	waitForListener(t, port, done)
	cancel()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("serve did not exit within 30s of its context being canceled")
	}

	return stdout, stderr, err
}

// waitForListener blocks until the port accepts a connection, the command
// exits, or the deadline passes.
func waitForListener(t *testing.T, port int, done <-chan struct{}) {
	t.Helper()

	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.After(30 * time.Second)

	var dialer net.Dialer

	for {
		select {
		case <-done:
			// The command exited before it ever listened, which is a legitimate
			// outcome for the tests that expect a failure. Let the caller read
			// the error.
			return
		case <-deadline:
			t.Fatalf("serve never listened on %s", address)
		default:
		}

		conn, err := dialer.Dial("tcp", address)
		if err == nil {
			_ = conn.Close()

			return
		}

		time.Sleep(20 * time.Millisecond)
	}
}

func TestServeStartsAndDrains(t *testing.T) {
	t.Parallel()

	_, stderr, err := serveUntilCanceled(t, migrated(t))
	if err != nil {
		t.Fatalf("serve: %v", err)
	}

	// Logs go to stderr so stdout stays clean for anything piping `pivot`.
	if !strings.Contains(stderr, "starting pivot") {
		t.Errorf("stderr = %q, want the startup line", stderr)
	}
}

func TestServeAutoMigratesByDefault(t *testing.T) {
	t.Parallel()

	// An empty database, which is what a container's very first start has.
	// autoMigrate defaults to true precisely so that works -- it is what makes
	// Part 15's zero-configuration first run possible, and it is off for a
	// clustered rollout where every replica would otherwise race.
	_, stderr, err := serveUntilCanceled(t, instance(t))
	if err != nil {
		t.Fatalf("serve against an empty database: %v", err)
	}

	if !strings.Contains(stderr, "migrations complete") {
		t.Errorf("stderr = %q, want it to report applying the migrations", stderr)
	}
}

func TestServeWarnsAboutPendingMigrationsRatherThanRefusing(t *testing.T) {
	t.Parallel()

	// Auto-migrate off, schema absent -- the clustered arrangement, where
	// migrations are run once and deliberately rather than by every replica.
	//
	// This asserts the documented choice in warnIfBehind rather than the one
	// that first seems obvious: it warns and starts, because a rolling deploy
	// legitimately runs old code against a newer schema for a window, and
	// refusing would turn that window into an outage.
	//
	// I wrote this test expecting a refusal. The code was right and the test
	// was wrong.
	env := instance(t)
	env["PIVOT_DATABASE_AUTO_MIGRATE"] = "false"

	_, stderr, err := serveUntilCanceled(t, env)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}

	if !strings.Contains(stderr, "pending migrations") {
		t.Errorf("stderr = %q, want a warning about the pending migrations", stderr)
	}

	// And the warning has to say what to do about it, or it is just noise.
	if !strings.Contains(stderr, "migrate up") {
		t.Errorf("stderr = %q, want the warning to name the fix", stderr)
	}
}

func TestServeRejectsAnUnreachableDatabase(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"PIVOT_DATABASE_URL": "postgres://nobody:nobody@127.0.0.1:1/nothing?sslmode=disable",
		"PIVOT_LOG_LEVEL":    "error",
		"PIVOT_SERVER_PORT":  strconv.Itoa(freePort(t)),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if _, _, err := runCtx(ctx, t, env, "serve"); err == nil {
		t.Error("serve started with no database to talk to")
	}
}
