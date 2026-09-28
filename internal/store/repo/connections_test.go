package repo_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/connectors"
	"github.com/Mmd4LIFE/pivot/internal/secrets"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

/*
Connections, and the password that must not be in the database.

The check that matters reads the column with SQL rather than through the
repository, because the repository is the thing under test: asking it whether
it encrypted something is asking the guard whether the door is locked.
*/

const connectionPassword = "the-warehouse-password"

func TestAConnectionPasswordIsSealedInTheDatabase(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		f := newConnectionFixture(t, db)

		created, err := f.repos.Connections.Create(f.ctx, repo.CreateConnection{
			Slug: "warehouse", Name: "Analytics warehouse", Kind: "postgres",
			Host: "db.internal", Port: 5432, Database: "analytics",
			Username: "pivot", Password: connectionPassword, SSLMode: "require",
			IsEnabled: true,
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}

		// What the caller gets back is the plaintext: above this layer a
		// password is a string, and the sealing is a property of storage.
		if created.Password != connectionPassword {
			t.Errorf("Create returned %q rather than the password it was given", created.Password)
		}

		// What the database holds is not.
		// Read without a placeholder, because the two engines spell them
		// differently and this fixture has exactly one connection.
		var stored string

		if qerr := db.QueryRowContext(context.Background(),
			"SELECT password FROM connections").Scan(&stored); qerr != nil {
			t.Fatalf("read the column: %v", qerr)
		}

		if stored == connectionPassword {
			t.Fatal("the password is in the database in the clear")
		}

		if !secrets.IsEnvelope(stored) {
			t.Errorf("the stored value is not an envelope: %s", stored)
		}

		// And reading it back through the repository returns it intact.
		fetched, err := f.repos.Connections.Get(f.ctx, created.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}

		if fetched.Password != connectionPassword {
			t.Errorf("Get returned %q", fetched.Password)
		}
	})
}

// The purpose string binds a ciphertext to its column, so a password lifted
// out of identity_providers cannot be pasted in here and decrypted.
func TestAConnectionPasswordCannotBeMovedFromAnotherColumn(t *testing.T) {
	t.Parallel()

	cipher := testCipher(t)

	elsewhere, err := cipher.Encrypt(repo.PurposeClientSecret, "an-oidc-secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if _, derr := cipher.Decrypt(repo.PurposeConnectionPassword, elsewhere); derr == nil {
		t.Error("a client secret decrypted as a connection password")
	}
}

func TestListingConnectionsIsScopedToTheOrganization(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		f := newConnectionFixture(t, db)

		if _, err := f.repos.Connections.Create(f.ctx, repo.CreateConnection{
			Slug: "ours", Name: "Ours", Kind: "postgres",
			Host: "h", Database: "d", Username: "u", Password: "p", IsEnabled: true,
		}); err != nil {
			t.Fatalf("create: %v", err)
		}

		conns, err := f.repos.Connections.List(f.ctx)
		if err != nil {
			t.Fatalf("list: %v", err)
		}

		if len(conns) != 1 {
			t.Fatalf("got %d connections, want 1", len(conns))
		}

		// Another organization's scope sees none of it.
		theirs, err := f.repos.Connections.List(f.otherCtx)
		if err != nil {
			t.Fatalf("list as the other org: %v", err)
		}

		if len(theirs) != 0 {
			t.Errorf("another organization can see %d of our connections", len(theirs))
		}
	})
}

// A slug is unique per organization while it lives, and reusable after a
// delete -- the same partial-index pattern as every other soft-deleted table.
func TestAConnectionSlugIsUniqueWhileItLives(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		f := newConnectionFixture(t, db)

		first, err := f.repos.Connections.Create(f.ctx, repo.CreateConnection{
			Slug: "warehouse", Name: "First", Kind: "postgres",
			Host: "h", Database: "d", Username: "u", IsEnabled: true,
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}

		_, err = f.repos.Connections.Create(f.ctx, repo.CreateConnection{
			Slug: "warehouse", Name: "Second", Kind: "postgres",
			Host: "h", Database: "d", Username: "u", IsEnabled: true,
		})

		if !errors.Is(err, repo.ErrDuplicate) {
			t.Fatalf("a duplicate slug gave %v, want ErrDuplicate", err)
		}

		if derr := f.repos.Connections.SoftDelete(f.ctx, first.ID); derr != nil {
			t.Fatalf("delete: %v", derr)
		}

		if _, rerr := f.repos.Connections.Create(f.ctx, repo.CreateConnection{
			Slug: "warehouse", Name: "Reused", Kind: "postgres",
			Host: "h", Database: "d", Username: "u", IsEnabled: true,
		}); rerr != nil {
			t.Errorf("the slug could not be reused after a delete: %v", rerr)
		}
	})
}

/*
A test result is recorded without bumping the version.

It is an observation about the world rather than a change somebody made. If it
bumped the version, a background health check would invalidate the form an
administrator has open, and their save would fail with a conflict they did
nothing to cause.
*/
func TestRecordingATestDoesNotLookLikeAnEdit(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		f := newConnectionFixture(t, db)

		created, err := f.repos.Connections.Create(f.ctx, repo.CreateConnection{
			Slug: "warehouse", Name: "Warehouse", Kind: "postgres",
			Host: "h", Database: "d", Username: "u", IsEnabled: true,
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}

		if created.LastTestedAt.Valid {
			t.Error("a new connection claims to have been tested")
		}

		if rerr := f.repos.Connections.RecordTest(f.ctx, created.ID, nil); rerr != nil {
			t.Fatalf("record a pass: %v", rerr)
		}

		after, err := f.repos.Connections.Get(f.ctx, created.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}

		if !after.LastTestedAt.Valid || !bool(after.LastTestOk) {
			t.Errorf("the passing test was not recorded: %+v", after.LastTestedAt)
		}

		if after.Version != created.Version {
			t.Errorf("recording a test bumped the version from %d to %d",
				created.Version, after.Version)
		}

		// A failure is recorded with its reason, which is the more useful of
		// the two to have on a list page.
		if rerr := f.repos.Connections.RecordTest(
			f.ctx, created.ID, errors.New("the host does not resolve")); rerr != nil {
			t.Fatalf("record a failure: %v", rerr)
		}

		failed, err := f.repos.Connections.Get(f.ctx, created.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}

		if bool(failed.LastTestOk) {
			t.Error("a failed test was recorded as a pass")
		}

		if !strings.Contains(failed.LastTestError, "does not resolve") {
			t.Errorf("the failure reason was not kept: %q", failed.LastTestError)
		}
	})
}

// A stale version is a conflict rather than a missing row, so the UI can say
// "somebody else changed this" instead of "it is gone".
func TestAStaleUpdateIsAConflict(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		f := newConnectionFixture(t, db)

		created, err := f.repos.Connections.Create(f.ctx, repo.CreateConnection{
			Slug: "warehouse", Name: "Warehouse", Kind: "postgres",
			Host: "h", Database: "d", Username: "u", Password: "p", IsEnabled: true,
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}

		update := repo.UpdateConnection{
			ID: created.ID, Slug: created.Slug, Name: "Renamed",
			Host: created.Host, Port: created.Port, Database: created.Database,
			Username: created.Username, Password: created.Password,
			IsEnabled: true, Version: created.Version,
		}

		if _, uerr := f.repos.Connections.Update(f.ctx, update); uerr != nil {
			t.Fatalf("update: %v", uerr)
		}

		// The same version again: somebody else has moved it on.
		if _, uerr := f.repos.Connections.Update(f.ctx, update); !errors.Is(uerr, repo.ErrConflict) {
			t.Errorf("a stale update gave %v, want ErrConflict", uerr)
		}
	})
}

// connectionFixture is two organizations and a repository set whose secrets
// cipher is real, so what reaches the database is what production would write.
type connectionFixture struct {
	repos    *repo.Repositories
	ctx      context.Context
	otherCtx context.Context
}

func newConnectionFixture(t *testing.T, db *store.DB) connectionFixture {
	t.Helper()

	repos := repo.New(db, repo.WithSecrets(testCipher(t)))
	sys := repos.System()
	ctx := context.Background()

	ours, err := sys.CreateOrganization(ctx, repo.CreateOrganization{Name: "Ours", Slug: "ours"})
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	theirs, err := sys.CreateOrganization(ctx, repo.CreateOrganization{Name: "Theirs", Slug: "theirs"})
	if err != nil {
		t.Fatalf("create the other org: %v", err)
	}

	return connectionFixture{
		repos:    repos,
		ctx:      tenant.WithScope(ctx, tenant.MustNewScope(ours.ID, uuid.NullUUID{})),
		otherCtx: tenant.WithScope(ctx, tenant.MustNewScope(theirs.ID, uuid.NullUUID{})),
	}
}

func testCipher(t *testing.T) secrets.Cipher {
	t.Helper()

	key, err := secrets.ParseKey("c2l4dGVlbi1ieXRlcy10aW1lcy10d28tZXhhY3RseSE=")
	if err != nil {
		t.Fatalf("parse test key: %v", err)
	}

	ring, err := secrets.NewKeyring(key)
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}

	return ring
}

/*
Every connector this build can open can also be stored.

The one test that would have caught Part 18-a. The connections table shipped
with `CHECK (kind IN ('postgres'))` and a comment calling the resulting
migration-per-connector deliberate; the very next connector was added without
one, so a MySQL connection could be configured and tested and was then refused
by the database on the way in. The connector's own tests never reached storage,
and the storage tests only ever named "postgres", so nothing looked.

Driven off the registry rather than a list, which is the whole point: a fourth
connector is covered by existing. If this fails, either a migration is missing
or a kind was registered under a name the schema will not take -- and the
failure says which kind, which is the thing that was missing before.
*/
func TestEveryRegisteredConnectorCanBeStored(t *testing.T) {
	t.Parallel()

	kinds := connectors.Kinds()
	if len(kinds) < 2 {
		t.Fatalf("the registry has %d kinds, so this proves nothing", len(kinds))
	}

	eachEngine(t, func(t *testing.T, db *store.DB) {
		f := newConnectionFixture(t, db)

		for _, kind := range kinds {
			created, err := f.repos.Connections.Create(f.ctx, repo.CreateConnection{
				// Slugged by kind, because they share an organization and a
				// slug is unique within one.
				Slug: "source-" + kind.String(),
				Name: "A " + kind.String() + " source",
				Kind: kind.String(),

				Host: "db.internal", Port: 5432, Database: "analytics",
				Username: "pivot", Password: connectionPassword,
				IsEnabled: true,
			})
			if err != nil {
				t.Errorf("a %s connection could not be stored: %v", kind, err)

				continue
			}

			if created.Kind != kind.String() {
				t.Errorf("stored kind = %q, want %q", created.Kind, kind)
			}
		}
	})
}

// A connection with no kind at all is still refused. Dropping the enumeration
// in 00007 loosened the constraint to what is actually true at this layer, and
// that is not the same as removing it.
func TestAConnectionStillNeedsAKind(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		f := newConnectionFixture(t, db)

		_, err := f.repos.Connections.Create(f.ctx, repo.CreateConnection{
			Slug: "nameless", Name: "No kind", Kind: "",
			Host: "db.internal", Database: "analytics", Username: "pivot",
			IsEnabled: true,
		})

		if err == nil {
			t.Fatal("a connection with no connector kind was stored")
		}
	})
}
