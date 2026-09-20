package repo_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/config"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

// PostgresURLEnv mirrors the store package's convention. Without it the
// PostgreSQL half skips rather than fails, so `make test` stays runnable with
// no Docker; `make test-all` and CI always set it.
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

	path := filepath.Join(t.TempDir(), "repo-test.db")

	db, err := store.Open(context.Background(), testDBConfig(path), discardLogger())
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

	schema := "repo_" + sanitize(t.Name())

	admin, err := store.Open(context.Background(), testDBConfig(url), discardLogger())
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = admin.Close() }()

	ctx := context.Background()
	if _, dropErr := admin.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); dropErr != nil {
		t.Fatalf("drop schema: %v", dropErr)
	}

	if _, createErr := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); createErr != nil {
		t.Fatalf("create schema: %v", createErr)
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

// eachEngine runs fn against both engines, already migrated.
func eachEngine(t *testing.T, fn func(t *testing.T, db *store.DB)) {
	t.Helper()

	run := func(t *testing.T, db *store.DB) {
		if err := store.Migrate(context.Background(), db, discardLogger()); err != nil {
			t.Fatalf("migrate: %v", err)
		}

		fn(t, db)
	}

	t.Run("sqlite", func(t *testing.T) {
		t.Parallel()
		run(t, openSQLite(t))
	})

	t.Run("postgres", func(t *testing.T) {
		t.Parallel()
		run(t, openPostgres(t))
	})
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

// --- behavior shared by every repository ----------------------------------

// A soft-deleted row must be invisible to ordinary reads, on both engines.
func TestSoftDeletedRowsAreInvisible(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		tt := setupTwoTenants(t, db)

		if err := tt.repos.Users.SoftDelete(tt.ctxA, tt.userA.ID); err != nil {
			t.Fatalf("SoftDelete: %v", err)
		}

		if _, err := tt.repos.Users.Get(tt.ctxA, tt.userA.ID); !errors.Is(err, repo.ErrNotFound) {
			t.Errorf("Get after soft delete = %v, want ErrNotFound", err)
		}

		if _, err := tt.repos.Users.GetByEmail(tt.ctxA, sharedEmail); !errors.Is(err, repo.ErrNotFound) {
			t.Errorf("GetByEmail after soft delete = %v, want ErrNotFound", err)
		}

		users, err := tt.repos.Users.List(tt.ctxA, 100, 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}

		if len(users) != 0 {
			t.Errorf("List returned %d users after the only one was deleted", len(users))
		}

		n, err := tt.repos.Users.Count(tt.ctxA)
		if err != nil {
			t.Fatalf("Count: %v", err)
		}

		if n != 0 {
			t.Errorf("Count = %d after soft delete, want 0", n)
		}

		// The row is still there, so the deletion is recoverable and the
		// partial unique index frees the email for reuse.
		if _, err := tt.repos.Users.Create(tt.ctxA, repo.CreateUser{
			Email: sharedEmail, Name: "Replacement", IsActive: true,
		}); err != nil {
			t.Errorf("recreating a user with a soft-deleted email failed: %v", err)
		}
	})
}

// Optimistic concurrency: the second of two writers must lose, not silently
// overwrite the first.
func TestStaleVersionUpdateConflicts(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		tt := setupTwoTenants(t, db)

		// Two readers both hold version 1.
		staleVersion := tt.userA.Version

		updated, err := tt.repos.Users.Update(tt.ctxA, repo.UpdateUser{
			ID: tt.userA.ID, Email: sharedEmail, Name: "First Writer",
			IsActive: true, Locale: "en", Timezone: "UTC", Version: staleVersion,
		})
		if err != nil {
			t.Fatalf("first update: %v", err)
		}

		if updated.Version != staleVersion+1 {
			t.Errorf("version = %d after update, want %d", updated.Version, staleVersion+1)
		}

		// The second writer still holds the old version.
		_, err = tt.repos.Users.Update(tt.ctxA, repo.UpdateUser{
			ID: tt.userA.ID, Email: sharedEmail, Name: "Second Writer",
			IsActive: true, Locale: "en", Timezone: "UTC", Version: staleVersion,
		})
		if !errors.Is(err, repo.ErrConflict) {
			t.Errorf("stale update = %v, want ErrConflict", err)
		}

		// The first writer's value must survive.
		after, err := tt.repos.Users.Get(tt.ctxA, tt.userA.ID)
		if err != nil {
			t.Fatalf("re-read: %v", err)
		}

		if after.Name != "First Writer" {
			t.Errorf("name = %q, want the first writer's value", after.Name)
		}
	})
}

// A login stamp must not bump the version, or a concurrent profile save would
// fail with a spurious conflict.
func TestRecordLoginDoesNotBumpVersion(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		tt := setupTwoTenants(t, db)

		if err := tt.repos.Users.RecordLogin(tt.ctxA, tt.userA.ID); err != nil {
			t.Fatalf("RecordLogin: %v", err)
		}

		after, err := tt.repos.Users.Get(tt.ctxA, tt.userA.ID)
		if err != nil {
			t.Fatalf("re-read: %v", err)
		}

		if after.Version != tt.userA.Version {
			t.Errorf("version = %d after login, want it unchanged at %d",
				after.Version, tt.userA.Version)
		}

		if !after.LastLoginAt.Valid {
			t.Error("LastLoginAt was not recorded")
		}

		// A profile save holding the pre-login version must still succeed.
		if _, err := tt.repos.Users.Update(tt.ctxA, repo.UpdateUser{
			ID: tt.userA.ID, Email: sharedEmail, Name: "Renamed",
			IsActive: true, Locale: "en", Timezone: "UTC", Version: tt.userA.Version,
		}); err != nil {
			t.Errorf("update after login = %v, want success", err)
		}
	})
}

// Duplicate emails within one tenant must be rejected and reported as a
// duplicate rather than as an opaque driver error.
func TestDuplicateEmailIsReportedAsDuplicate(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		tt := setupTwoTenants(t, db)

		_, err := tt.repos.Users.Create(tt.ctxA, repo.CreateUser{
			Email: sharedEmail, Name: "Duplicate", IsActive: true,
		})
		if !errors.Is(err, repo.ErrDuplicate) {
			t.Errorf("duplicate email = %v, want ErrDuplicate", err)
		}
	})
}

// Change events carry the acting user, which is the reason they exist rather
// than database triggers.
func TestChangeEventsArePublished(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		repos := repo.New(db)

		var (
			mu     sync.Mutex
			events []repo.ChangeEvent
		)

		repos.Events().Subscribe(func(_ context.Context, e repo.ChangeEvent) {
			mu.Lock()
			defer mu.Unlock()

			events = append(events, e)
		})

		org, err := repos.System().CreateOrganization(context.Background(),
			repo.CreateOrganization{Name: "Events", Slug: "events"})
		if err != nil {
			t.Fatalf("create org: %v", err)
		}

		actor := uuid.NullUUID{UUID: uuid.Must(uuid.NewV7()), Valid: true}
		ctx := tenant.WithScope(context.Background(), tenant.MustNewScope(org.ID, actor))

		user, err := repos.Users.Create(ctx, repo.CreateUser{
			Email: "e@example.com", Name: "E", IsActive: true,
		})
		if err != nil {
			t.Fatalf("create user: %v", err)
		}

		if err := repos.Users.SoftDelete(ctx, user.ID); err != nil {
			t.Fatalf("soft delete: %v", err)
		}

		mu.Lock()
		defer mu.Unlock()

		if len(events) != 3 {
			t.Fatalf("got %d events, want 3 (org created, user created, user deleted): %+v",
				len(events), events)
		}

		userCreated := events[1]
		if userCreated.Kind != repo.ChangeCreated || userCreated.EntityType != "user" {
			t.Errorf("event[1] = %+v, want a user creation", userCreated)
		}

		if userCreated.OrgID != org.ID {
			t.Errorf("event orgID = %v, want %v", userCreated.OrgID, org.ID)
		}

		if !userCreated.ActorID.Valid || userCreated.ActorID.UUID != actor.UUID {
			t.Errorf("event actor = %+v, want %+v — the acting user is the whole "+
				"reason these are application events and not database triggers",
				userCreated.ActorID, actor)
		}

		if events[2].Kind != repo.ChangeDeleted {
			t.Errorf("event[2] kind = %v, want deleted", events[2].Kind)
		}
	})
}

// A panicking subscriber must not fail the write that triggered it: the write
// has already committed by then.
func TestPanickingSubscriberDoesNotFailTheWrite(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		repos := repo.New(db)

		repos.Events().Subscribe(func(context.Context, repo.ChangeEvent) {
			panic("subscriber blew up")
		})

		var delivered bool

		repos.Events().Subscribe(func(context.Context, repo.ChangeEvent) {
			delivered = true
		})

		if _, err := repos.System().CreateOrganization(context.Background(),
			repo.CreateOrganization{Name: "Panic", Slug: "panic"}); err != nil {
			t.Fatalf("create org with a panicking subscriber = %v, want success", err)
		}

		if !delivered {
			t.Error("a panicking subscriber prevented later subscribers from running")
		}
	})
}
