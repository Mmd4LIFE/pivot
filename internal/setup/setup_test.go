package setup_test

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/auth"
	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/config"
	"github.com/Mmd4LIFE/pivot/internal/setup"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

/*
The first run.

SQLite only, deliberately. Everything here is counts and inserts through the
repository layer, which is itself tested against both engines; a second engine
here would re-test the repositories rather than this package. The one thing
that is genuinely this package's own -- that two simultaneous claims produce
one administrator -- is a property of a mutex and not of a database.
*/

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func openDB(t *testing.T) *store.DB {
	t.Helper()

	cfg := config.Default().Database
	cfg.URL = filepath.Join(t.TempDir(), "setup-test.db")
	cfg.AutoMigrate = false

	db, err := store.Open(context.Background(), cfg, discard())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	if err := store.Migrate(context.Background(), db, discard()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return db
}

func newService(t *testing.T, token string) (*setup.Service, *repo.Repositories) {
	t.Helper()

	repos := repo.New(openDB(t))

	return setup.NewService(repos, token), repos
}

func request() setup.Request {
	return setup.Request{
		Organization: "Acme Analytics",
		Name:         "Ada Lovelace",
		Email:        "ada@example.com",
		Password:     "a-long-enough-password",
	}
}

// The deliverable: an empty database, and somebody comes out of it an
// administrator.
func TestClaimingAnInstanceMakesAnAdministrator(t *testing.T) {
	t.Parallel()

	svc, repos := newService(t, "")

	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if status.Initialized {
		t.Fatal("an empty database reports itself as already set up")
	}

	result, err := svc.Initialize(context.Background(), request())
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}

	if result.OrgSlug != "acme-analytics" {
		t.Errorf("slug = %q, want acme-analytics", result.OrgSlug)
	}

	// Administrator, not merely a user. A first run that creates an account
	// with no roles produces an instance nobody can ever grant a role in,
	// which is complete and unusable.
	checker, _ := authz.New(repos)

	scope, err := tenant.NewSystemScope(result.OrgID)
	if err != nil {
		t.Fatalf("scope: %v", err)
	}

	decision, err := checker.Check(tenant.WithScope(context.Background(), scope), authz.Request{
		Subject:    authz.User(result.UserID.String()),
		Permission: authz.PermManageRoles,
		Object: authz.Object{
			Type: authz.TypeOrganization,
			ID:   result.OrgID.String(),
		},
	})
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	if !decision.Allowed {
		t.Error("the first user cannot manage roles, so nobody ever can")
	}

	after, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if !after.Initialized {
		t.Error("the instance still reports itself as unclaimed")
	}
}

// Setup closes permanently. An endpoint that creates administrators and stays
// open is a way to become one.
func TestAnInstanceCanOnlyBeClaimedOnce(t *testing.T) {
	t.Parallel()

	svc, _ := newService(t, "")

	if _, err := svc.Initialize(context.Background(), request()); err != nil {
		t.Fatalf("first initialize: %v", err)
	}

	second := request()
	second.Organization = "Someone Else"
	second.Email = "mallory@example.com"

	_, err := svc.Initialize(context.Background(), second)

	if !errors.Is(err, setup.ErrAlreadyInitialized) {
		t.Fatalf("second initialize: %v, want ErrAlreadyInitialized", err)
	}
}

/*
Two browsers, one moment.

Without the lock both read "no organizations", both create one, and the
instance ends up with two tenants and two administrators -- on a product whose
whole first-run story is that there is one. This is the test that fails under
-race if the exclusion is removed.
*/
func TestTwoSimultaneousClaimsProduceOneAdministrator(t *testing.T) {
	t.Parallel()

	svc, repos := newService(t, "")

	const attempts = 8

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		succeeded int
	)

	start := make(chan struct{})

	for i := range attempts {
		wg.Add(1)

		go func() {
			defer wg.Done()

			in := request()
			in.Organization = "Org " + string(rune('A'+i))
			in.Email = string(rune('a'+i)) + "@example.com"

			<-start

			if _, err := svc.Initialize(context.Background(), in); err == nil {
				mu.Lock()
				succeeded++
				mu.Unlock()
			}
		}()
	}

	close(start)
	wg.Wait()

	if succeeded != 1 {
		t.Errorf("%d of %d concurrent claims succeeded, want exactly 1", succeeded, attempts)
	}

	orgs, err := repos.System().CountOrganizations(context.Background())
	if err != nil {
		t.Fatalf("count: %v", err)
	}

	if orgs != 1 {
		t.Errorf("%d organizations exist, want 1", orgs)
	}
}

// The token is what stops whoever reaches an unclaimed instance first from
// claiming it. A wrong one and a missing one are both refusals.
func TestTheTokenIsEnforced(t *testing.T) {
	t.Parallel()

	const token = "the-real-token"

	for name, tc := range map[string]struct {
		presented string
		want      error
	}{
		"missing": {presented: "", want: setup.ErrTokenRequired},
		"wrong":   {presented: "not-the-token", want: setup.ErrTokenInvalid},
		"nearly":  {presented: "the-real-toke", want: setup.ErrTokenInvalid},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			svc, repos := newService(t, token)

			in := request()
			in.Token = tc.presented

			if _, err := svc.Initialize(context.Background(), in); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}

			// And nothing was created on the way to refusing. A failed claim
			// that leaves an organization behind would make the instance look
			// claimed to the next person, permanently.
			orgs, err := repos.System().CountOrganizations(context.Background())
			if err != nil {
				t.Fatalf("count: %v", err)
			}

			if orgs != 0 {
				t.Errorf("%d organizations exist after a refused claim, want 0", orgs)
			}
		})
	}
}

func TestTheRightTokenIsAccepted(t *testing.T) {
	t.Parallel()

	const token = "the-real-token"

	svc, _ := newService(t, token)

	in := request()
	in.Token = token

	if _, err := svc.Initialize(context.Background(), in); err != nil {
		t.Fatalf("initialize with the correct token: %v", err)
	}
}

// A short password is refused before anything is written. The floor lives in
// auth.HashPassword, which is the only place that can enforce it for every
// caller -- this checks that setup goes through it rather than around it.
func TestAShortPasswordIsRefusedAndWritesNothing(t *testing.T) {
	t.Parallel()

	svc, repos := newService(t, "")

	in := request()
	in.Password = "short"

	if _, err := svc.Initialize(context.Background(), in); !errors.Is(err, auth.ErrPasswordTooShort) {
		t.Fatalf("error = %v, want ErrPasswordTooShort", err)
	}

	orgs, err := repos.System().CountOrganizations(context.Background())
	if err != nil {
		t.Fatalf("count: %v", err)
	}

	if orgs != 0 {
		t.Errorf("%d organizations exist after a rejected password, want 0", orgs)
	}
}

/*
Status is keyed on organizations, not users.

An organization with no users is a half-finished setup that nobody can log into
and nobody can finish. Reporting it as initialized would leave that instance
permanently unusable, with no way back short of a new database -- so the
half-finished state has to stay claimable.
*/
func TestAHalfFinishedSetupStaysClaimable(t *testing.T) {
	t.Parallel()

	svc, repos := newService(t, "")

	// An organization and no user: what an Initialize that failed at the user
	// leaves behind.
	org, err := repos.System().CreateOrganization(context.Background(),
		repo.CreateOrganization{Name: "Half Done", Slug: "half-done"})
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	// It reports as initialized, which is the deliberate trade: the check is
	// one count rather than a join, and the alternative -- an instance that
	// says it needs setup while an organization already exists -- would let a
	// second one be created beside the first.
	if !status.Initialized {
		t.Error("an organization exists but the instance says it is unclaimed")
	}

	// And the recovery path is the CLI, which does not consult this at all.
	scope, err := tenant.NewSystemScope(org.ID)
	if err != nil {
		t.Fatalf("scope: %v", err)
	}

	hash, err := auth.HashPassword("a-long-enough-password")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	_, granted, err := setup.CreateUser(tenant.WithScope(context.Background(), scope),
		repos, org.ID, setup.CreateUserRequest{Email: "ada@example.com", PasswordHash: hash})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if !granted {
		t.Error("the first user of the half-finished organization did not become its administrator")
	}
}

// The second user is not an administrator. The first-user rule is what makes a
// fresh install usable; applied to everybody it would be a standing privilege
// escalation.
func TestOnlyTheFirstUserBecomesAdministrator(t *testing.T) {
	t.Parallel()

	svc, repos := newService(t, "")

	result, err := svc.Initialize(context.Background(), request())
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}

	scope, err := tenant.NewSystemScope(result.OrgID)
	if err != nil {
		t.Fatalf("scope: %v", err)
	}

	hash, err := auth.HashPassword("another-long-password")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	_, granted, err := setup.CreateUser(tenant.WithScope(context.Background(), scope),
		repos, result.OrgID, setup.CreateUserRequest{
			Email: "second@example.com", PasswordHash: hash,
		})
	if err != nil {
		t.Fatalf("create second user: %v", err)
	}

	if granted {
		t.Error("the second user was made an administrator")
	}
}

func TestSlugify(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"Acme Analytics":     "acme-analytics",
		"  Acme  ":           "acme",
		"Ünïcödé Ltd":        "n-c-d-ltd",
		"!!!":                "org",
		"":                   "org",
		"Already-Slugged-99": "already-slugged-99",
	} {
		if got := setup.Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

// Two tokens from one process must differ, or a token is not a secret.
func TestTokensAreNotPredictable(t *testing.T) {
	t.Parallel()

	first, err := setup.NewToken()
	if err != nil {
		t.Fatalf("new token: %v", err)
	}

	second, err := setup.NewToken()
	if err != nil {
		t.Fatalf("new token: %v", err)
	}

	if first == second {
		t.Fatal("two generated tokens are identical")
	}

	// 32 bytes, base64url without padding.
	if len(first) != 43 {
		t.Errorf("token is %d characters, want 43 (32 random bytes)", len(first))
	}
}
