package auth_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/auth"
	"github.com/Mmd4LIFE/pivot/internal/config"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

const PostgresURLEnv = "PIVOT_TEST_POSTGRES_URL"

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func testDBConfig(url string) config.DatabaseConfig {
	cfg := config.Default().Database
	cfg.URL = url
	cfg.AutoMigrate = false

	return cfg
}

func openSQLite(t *testing.T) *store.DB {
	t.Helper()

	db, err := store.Open(context.Background(),
		testDBConfig(filepath.Join(t.TempDir(), "auth-test.db")), discardLogger())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return db
}

func openPostgres(t *testing.T) *store.DB {
	t.Helper()

	url := os.Getenv(PostgresURLEnv)
	if url == "" {
		t.Skipf("%s not set; run `make test-all` to include PostgreSQL", PostgresURLEnv)
	}

	schema := "auth_" + sanitize(t.Name())

	admin, err := store.Open(context.Background(), testDBConfig(url), discardLogger())
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = admin.Close() }()

	ctx := context.Background()
	if _, derr := admin.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); derr != nil {
		t.Fatalf("drop schema: %v", derr)
	}

	if _, cerr := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); cerr != nil {
		t.Fatalf("create schema: %v", cerr)
	}

	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}

	db, err := store.Open(ctx, testDBConfig(url+sep+"search_path="+schema), discardLogger())
	if err != nil {
		t.Fatalf("open postgres schema: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()

		cleanup, cerr := store.Open(context.Background(), testDBConfig(url), discardLogger())
		if cerr != nil {
			return
		}
		defer func() { _ = cleanup.Close() }()

		_, _ = cleanup.ExecContext(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	})

	return db
}

func sanitize(in string) string {
	out := make([]rune, 0, len(in))

	for _, r := range in {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
		default:
			out = append(out, '_')
		}
	}

	if len(out) > 40 {
		out = out[:40]
	}

	return string(out)
}

// fixture is a migrated database with one organization and one user.
type fixture struct {
	db    *store.DB
	repos *repo.Repositories
	svc   *auth.Service
	logs  *bytes.Buffer

	org  model.Organization
	user model.User
	ctx  context.Context

	// clock is advanced by tests instead of sleeping.
	clock time.Time
}

const (
	testEmail    = "ada@example.com"
	testPassword = "a-sufficiently-long-password"
)

func newFixture(t *testing.T, db *store.DB) *fixture {
	t.Helper()

	if err := store.Migrate(context.Background(), db, discardLogger()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Logs go to a buffer so a test can assert nothing secret reached them.
	var logs bytes.Buffer

	log := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	repos := repo.New(db)

	org, err := repos.System().CreateOrganization(context.Background(),
		repo.CreateOrganization{Name: "Acme", Slug: "acme"})
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	scope := tenant.MustNewScope(org.ID, uuid.NullUUID{})
	ctx := tenant.WithScope(context.Background(), scope)

	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	user, err := repos.Users.Create(ctx, repo.CreateUser{
		Email: testEmail, Name: "Ada", PasswordHash: hash, IsActive: true,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	f := &fixture{
		db: db, repos: repos, logs: &logs,
		org: org, user: user, ctx: ctx,
		clock: time.Now(),
	}

	f.svc = auth.NewService(repos, auth.DefaultPolicy(), log)
	f.svc.SetClock(func() time.Time { return f.clock })

	return f
}

func (f *fixture) advance(d time.Duration) { f.clock = f.clock.Add(d) }

func (f *fixture) login(t *testing.T, password string) (*auth.LoginResult, error) {
	t.Helper()

	return f.svc.Login(context.Background(), auth.Credentials{
		OrgID: f.org.ID, Email: testEmail, Password: password,
		IP: "127.0.0.1", UserAgent: "test",
	})
}

func eachEngine(t *testing.T, fn func(t *testing.T, f *fixture)) {
	t.Helper()

	t.Run("sqlite", func(t *testing.T) {
		t.Parallel()
		fn(t, newFixture(t, openSQLite(t)))
	})

	t.Run("postgres", func(t *testing.T) {
		t.Parallel()
		fn(t, newFixture(t, openPostgres(t)))
	})
}

// --- the Done-when criteria ------------------------------------------------

func TestLoginThenAuthenticate(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, f *fixture) {
		res, err := f.login(t, testPassword)
		if err != nil {
			t.Fatalf("Login: %v", err)
		}

		if res.Token == "" {
			t.Fatal("login returned no token")
		}

		if res.User.ID != f.user.ID {
			t.Errorf("login returned user %v, want %v", res.User.ID, f.user.ID)
		}

		// The session row must hold a hash, never the token.
		if res.Session.TokenHash == res.Token {
			t.Error("the stored session holds the raw token")
		}

		auth1, err := f.svc.Authenticate(context.Background(), res.Token)
		if err != nil {
			t.Fatalf("Authenticate: %v", err)
		}

		if auth1.Scope.OrgID() != f.org.ID {
			t.Errorf("resolved org = %v, want %v", auth1.Scope.OrgID(), f.org.ID)
		}

		if !auth1.Scope.ActorID().Valid || auth1.Scope.ActorID().UUID != f.user.ID {
			t.Errorf("resolved actor = %+v, want user %v", auth1.Scope.ActorID(), f.user.ID)
		}
	})
}

// Logout must take effect on the very next request. That immediacy is the
// entire reason sessions are server-side rather than stateless tokens.
func TestLogoutRevokesImmediately(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, f *fixture) {
		res, err := f.login(t, testPassword)
		if err != nil {
			t.Fatalf("Login: %v", err)
		}

		if _, aerr := f.svc.Authenticate(context.Background(), res.Token); aerr != nil {
			t.Fatalf("the session should be valid before logout: %v", aerr)
		}

		if lerr := f.svc.Logout(context.Background(), res.Token); lerr != nil {
			t.Fatalf("Logout: %v", lerr)
		}

		// No clock advance: the very next call must fail.
		if _, aerr := f.svc.Authenticate(context.Background(), res.Token); !errors.Is(aerr, auth.ErrSessionInvalid) {
			t.Errorf("a revoked session authenticated: %v", aerr)
		}
	})
}

// Every credential failure must be indistinguishable. Unknown account, wrong
// password and disabled account all return the same error.
func TestFailedLoginsAreIndistinguishable(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, f *fixture) {
		t.Run("wrong password", func(t *testing.T) {
			_, err := f.login(t, "the wrong password entirely")
			if !errors.Is(err, auth.ErrInvalidCredentials) {
				t.Errorf("wrong password gave %v, want ErrInvalidCredentials", err)
			}
		})

		t.Run("unknown account", func(t *testing.T) {
			_, err := f.svc.Login(context.Background(), auth.Credentials{
				OrgID: f.org.ID, Email: "nobody@example.com", Password: testPassword,
			})
			if !errors.Is(err, auth.ErrInvalidCredentials) {
				t.Errorf("unknown account gave %v, want ErrInvalidCredentials", err)
			}
		})

		t.Run("disabled account", func(t *testing.T) {
			if _, err := f.repos.Users.Update(f.ctx, repo.UpdateUser{
				ID: f.user.ID, Email: testEmail, Name: "Ada",
				IsActive: false, Locale: "en", Timezone: "UTC", Version: f.user.Version,
			}); err != nil {
				t.Fatalf("disable user: %v", err)
			}

			_, err := f.login(t, testPassword)
			if !errors.Is(err, auth.ErrInvalidCredentials) {
				t.Errorf("disabled account gave %v, want ErrInvalidCredentials — "+
					"saying 'this account is disabled' confirms the address exists", err)
			}
		})
	})
}

// Ten failures lock the account; the right password is then refused with a
// distinguishable error, because a user who cannot log in deserves to know why.
func TestLockoutAfterRepeatedFailures(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, f *fixture) {
		policy := auth.DefaultPolicy()

		for i := range policy.MaxFailedAttempts {
			_, err := f.login(t, "wrong")
			if !errors.Is(err, auth.ErrInvalidCredentials) {
				t.Fatalf("attempt %d gave %v, want ErrInvalidCredentials", i+1, err)
			}
		}

		// The correct password must now be refused, and told apart from a
		// wrong one.
		_, err := f.login(t, testPassword)
		if !errors.Is(err, auth.ErrAccountLocked) {
			t.Fatalf("after %d failures the correct password gave %v, want ErrAccountLocked",
				policy.MaxFailedAttempts, err)
		}

		// Waiting out the lockout restores access.
		f.advance(policy.LockoutDuration + time.Second)

		if _, lerr := f.login(t, testPassword); lerr != nil {
			t.Errorf("login after the lockout expired gave %v, want success", lerr)
		}
	})
}

// A lockout applies to addresses that do not exist too, or it becomes a
// user-enumeration oracle: an attacker learns which addresses are real by
// seeing which ones start refusing.
func TestLockoutAppliesToUnknownAccounts(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, f *fixture) {
		policy := auth.DefaultPolicy()
		unknown := "ghost@example.com"

		for range policy.MaxFailedAttempts {
			_, _ = f.svc.Login(context.Background(), auth.Credentials{
				OrgID: f.org.ID, Email: unknown, Password: "wrong",
			})
		}

		_, err := f.svc.Login(context.Background(), auth.Credentials{
			OrgID: f.org.ID, Email: unknown, Password: "wrong",
		})
		if !errors.Is(err, auth.ErrAccountLocked) {
			t.Errorf("an unknown address was not locked out (%v); the lockout "+
				"distinguishes real accounts from fake ones", err)
		}
	})
}

// A successful login clears the counter, so occasional typos never accumulate
// into a lockout.
func TestSuccessfulLoginClearsFailures(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, f *fixture) {
		for range 5 {
			_, _ = f.login(t, "wrong")
		}

		if _, err := f.login(t, testPassword); err != nil {
			t.Fatalf("login with the correct password: %v", err)
		}

		// Five more failures must not lock, because the counter reset.
		for i := range 5 {
			if _, err := f.login(t, "wrong"); !errors.Is(err, auth.ErrInvalidCredentials) {
				t.Fatalf("attempt %d gave %v; the counter was not cleared", i+1, err)
			}
		}
	})
}

// Case variation must not reset the lockout counter, or the lockout is
// decorative.
func TestLockoutIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, f *fixture) {
		policy := auth.DefaultPolicy()

		for i := range policy.MaxFailedAttempts {
			email := testEmail
			if i%2 == 0 {
				email = strings.ToUpper(testEmail)
			}

			_, _ = f.svc.Login(context.Background(), auth.Credentials{
				OrgID: f.org.ID, Email: email, Password: "wrong",
			})
		}

		if _, err := f.login(t, testPassword); !errors.Is(err, auth.ErrAccountLocked) {
			t.Errorf("alternating case avoided the lockout (%v)", err)
		}
	})
}

// --- session lifetime ------------------------------------------------------

func TestIdleTimeoutExpiresSession(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, f *fixture) {
		res, err := f.login(t, testPassword)
		if err != nil {
			t.Fatalf("Login: %v", err)
		}

		f.advance(auth.DefaultPolicy().IdleTimeout + time.Minute)

		if _, aerr := f.svc.Authenticate(context.Background(), res.Token); !errors.Is(aerr, auth.ErrSessionInvalid) {
			t.Errorf("an idle-expired session authenticated: %v", aerr)
		}
	})
}

// Use pushes the idle expiry forward, so an active session does not die
// mid-work.
func TestActivityExtendsIdleExpiry(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, f *fixture) {
		policy := auth.DefaultPolicy()

		res, err := f.login(t, testPassword)
		if err != nil {
			t.Fatalf("Login: %v", err)
		}

		// Stay active across more than one idle window.
		for range 3 {
			f.advance(policy.IdleTimeout / 2)

			if _, aerr := f.svc.Authenticate(context.Background(), res.Token); aerr != nil {
				t.Fatalf("an active session expired: %v", aerr)
			}
		}
	})
}

// The absolute cap is never extended: a stolen token must not stay valid
// forever just because the thief keeps using it.
func TestAbsoluteTimeoutEndsEvenActiveSessions(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, f *fixture) {
		policy := auth.DefaultPolicy()

		res, err := f.login(t, testPassword)
		if err != nil {
			t.Fatalf("Login: %v", err)
		}

		// Keep it active right up to the absolute cap.
		for f.clock.Before(res.Session.AbsoluteExpiresAt.Add(-policy.IdleTimeout / 2)) {
			f.advance(policy.IdleTimeout / 2)

			if _, aerr := f.svc.Authenticate(context.Background(), res.Token); aerr != nil {
				t.Fatalf("session died before the absolute cap: %v", aerr)
			}
		}

		f.advance(policy.IdleTimeout)

		if _, aerr := f.svc.Authenticate(context.Background(), res.Token); !errors.Is(aerr, auth.ErrSessionInvalid) {
			t.Error("an actively used session survived its absolute expiry")
		}
	})
}

// A password change must end every existing session — that is the point of
// changing it after a suspected compromise.
func TestPasswordChangeRevokesAllSessions(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, f *fixture) {
		first, err := f.login(t, testPassword)
		if err != nil {
			t.Fatalf("Login: %v", err)
		}

		second, err := f.login(t, testPassword)
		if err != nil {
			t.Fatalf("second Login: %v", err)
		}

		if serr := f.svc.SetPassword(f.ctx, f.user.ID, "a-brand-new-long-password"); serr != nil {
			t.Fatalf("SetPassword: %v", serr)
		}

		for name, token := range map[string]string{"first": first.Token, "second": second.Token} {
			if _, aerr := f.svc.Authenticate(context.Background(), token); !errors.Is(aerr, auth.ErrSessionInvalid) {
				t.Errorf("the %s session survived a password change: %v", name, aerr)
			}
		}

		// The new password works; the old one does not.
		if _, lerr := f.login(t, "a-brand-new-long-password"); lerr != nil {
			t.Errorf("login with the new password: %v", lerr)
		}

		if _, lerr := f.login(t, testPassword); !errors.Is(lerr, auth.ErrInvalidCredentials) {
			t.Errorf("the old password still works: %v", lerr)
		}
	})
}

func TestSweepRemovesExpiredSessions(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, f *fixture) {
		res, err := f.login(t, testPassword)
		if err != nil {
			t.Fatalf("Login: %v", err)
		}

		sessions, err := f.repos.Sessions.List(f.ctx, f.user.ID)
		if err != nil {
			t.Fatalf("List: %v", err)
		}

		if len(sessions) != 1 {
			t.Fatalf("expected one active session, got %d", len(sessions))
		}

		f.advance(auth.DefaultPolicy().AbsoluteTimeout + time.Hour)

		removed, _, serr := f.svc.Sweep(context.Background(), time.Hour)
		if serr != nil {
			t.Fatalf("Sweep: %v", serr)
		}

		if removed < 1 {
			t.Errorf("the sweep removed %d sessions, want at least 1", removed)
		}

		if _, aerr := f.svc.Authenticate(context.Background(), res.Token); !errors.Is(aerr, auth.ErrSessionInvalid) {
			t.Error("a swept session still authenticates")
		}
	})
}

// --- the leak check --------------------------------------------------------

// Password hashes and session tokens must never reach the logs. This is the
// assertion the checklist names, and it covers the whole login path rather
// than a single call.
func TestSecretsNeverReachTheLogs(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, f *fixture) {
		res, err := f.login(t, testPassword)
		if err != nil {
			t.Fatalf("Login: %v", err)
		}

		// Exercise the failure paths too, which is where a careless log line
		// is most likely to have been added.
		_, _ = f.login(t, "wrong password")
		_, _ = f.svc.Login(context.Background(), auth.Credentials{
			OrgID: f.org.ID, Email: "nobody@example.com", Password: testPassword,
		})
		_ = f.svc.Logout(context.Background(), res.Token)

		reread, err := f.repos.Users.Get(f.ctx, f.user.ID)
		if err != nil {
			t.Fatalf("re-read user: %v", err)
		}

		logs := f.logs.String()

		for name, secret := range map[string]string{
			"the password":      testPassword,
			"the session token": res.Token,
			"the stored hash":   reread.PasswordHash.String,
		} {
			if secret == "" {
				t.Fatalf("%s is empty; the test is not checking anything", name)
			}

			if strings.Contains(logs, secret) {
				t.Errorf("%s appears in the logs", name)
			}
		}

		// The hash prefix alone would be enough to confirm a leak of the
		// stored credential.
		if strings.Contains(logs, "$argon2id$") {
			t.Error("an argon2 hash appears in the logs")
		}
	})
}
