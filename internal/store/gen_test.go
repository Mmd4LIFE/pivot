package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/dbtypes"
	"github.com/Mmd4LIFE/pivot/internal/store/gen/lite"
	"github.com/Mmd4LIFE/pivot/internal/store/gen/pg"
)

// The generated packages are structurally identical by construction — the
// dbtypes overrides in sqlc.yaml exist to make them so — but "identical types"
// is a compile-time claim. These tests are the runtime half: they prove a value
// written through one engine comes back as the same Go value, which is where
// TEXT-vs-timestamptz and INTEGER-vs-boolean would otherwise diverge silently.
//
// querier is the shared surface. Both generated Queriers satisfy it, so every
// test below is written once and executed against both engines.
type querier interface {
	CreateOrganization(context.Context, pgOrgParams) (organization, error)
}

// Rather than invent an abstraction the generated code does not have, each test
// runs through a small adapter. Part 4 replaces these with the real repository
// layer; here they exist only so one test body covers two engines.

type organization struct {
	ID        uuid.UUID
	Name      string
	Slug      string
	Settings  dbtypes.JSON
	Plan      string
	CreatedAt dbtypes.Time
	UpdatedAt dbtypes.Time
	CreatedBy uuid.NullUUID
	DeletedAt dbtypes.NullTime
	Version   int64
}

type pgOrgParams struct {
	ID        uuid.UUID
	Name      string
	Slug      string
	Settings  dbtypes.JSON
	Plan      string
	CreatedBy uuid.NullUUID
	UpdatedBy uuid.NullUUID
}

type pgAdapter struct{ q *pg.Queries }

func (a pgAdapter) CreateOrganization(ctx context.Context, p pgOrgParams) (organization, error) {
	row, err := a.q.CreateOrganization(ctx, pg.CreateOrganizationParams(p))
	if err != nil {
		return organization{}, err
	}

	return organization{
		ID: row.ID, Name: row.Name, Slug: row.Slug, Settings: row.Settings,
		Plan: row.Plan, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		CreatedBy: row.CreatedBy, DeletedAt: row.DeletedAt, Version: row.Version,
	}, nil
}

type liteAdapter struct{ q *lite.Queries }

func (a liteAdapter) CreateOrganization(ctx context.Context, p pgOrgParams) (organization, error) {
	row, err := a.q.CreateOrganization(ctx, lite.CreateOrganizationParams(p))
	if err != nil {
		return organization{}, err
	}

	return organization{
		ID: row.ID, Name: row.Name, Slug: row.Slug, Settings: row.Settings,
		Plan: row.Plan, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		CreatedBy: row.CreatedBy, DeletedAt: row.DeletedAt, Version: row.Version,
	}, nil
}

func queriesFor(t *testing.T, db *store.DB) querier {
	t.Helper()

	if db.IsPostgres() {
		return pgAdapter{pg.New(db.DB)}
	}

	return liteAdapter{lite.New(db.DB)}
}

// eachMigratedEngine runs fn against both engines, already migrated.
func eachMigratedEngine(t *testing.T, fn func(t *testing.T, db *store.DB)) {
	t.Helper()

	eachEngine(t, func(t *testing.T, db *store.DB) {
		migrated(t, db)
		fn(t, db)
	})
}

// The core claim: a UUID, a JSON document, a timestamp and a version written
// through the generated code come back as the same Go values on both engines.
func TestOrganizationRoundTrip(t *testing.T) {
	t.Parallel()

	eachMigratedEngine(t, func(t *testing.T, db *store.DB) {
		q := queriesFor(t, db)
		ctx := context.Background()

		id := uuid.Must(uuid.NewV7())
		actor := uuid.NullUUID{UUID: uuid.Must(uuid.NewV7()), Valid: true}

		settings, err := dbtypes.MarshalJSONValue(map[string]any{
			"theme": "dark", "retentionDays": 90,
		})
		if err != nil {
			t.Fatalf("marshal settings: %v", err)
		}

		got, err := q.CreateOrganization(ctx, pgOrgParams{
			ID: id, Name: "Acme", Slug: "acme", Settings: settings,
			Plan: "enterprise", CreatedBy: actor, UpdatedBy: actor,
		})
		if err != nil {
			t.Fatalf("CreateOrganization on %s: %v", db.Engine(), err)
		}

		if got.ID != id {
			t.Errorf("ID = %v, want %v", got.ID, id)
		}

		if got.Name != "Acme" || got.Slug != "acme" || got.Plan != "enterprise" {
			t.Errorf("scalar fields did not round-trip: %+v", got)
		}

		if !got.CreatedBy.Valid || got.CreatedBy.UUID != actor.UUID {
			t.Errorf("CreatedBy = %+v, want %+v", got.CreatedBy, actor)
		}

		// JSON must survive as a document, not as an escaped string.
		var decoded map[string]any
		if err := got.Settings.Unmarshal(&decoded); err != nil {
			t.Fatalf("settings did not round-trip as JSON on %s: %v (raw %q)",
				db.Engine(), err, string(got.Settings))
		}

		if decoded["theme"] != "dark" {
			t.Errorf("settings.theme = %v, want %q", decoded["theme"], "dark")
		}

		// The column DEFAULT populated created_at; it must parse on both engines.
		if got.CreatedAt.IsZero() {
			t.Error("CreatedAt is zero; the column default did not round-trip")
		}

		if age := time.Since(got.CreatedAt.Time); age < -time.Minute || age > time.Hour {
			t.Errorf("CreatedAt = %v, which is not close to now", got.CreatedAt)
		}

		if got.CreatedAt.Location() != time.UTC {
			t.Errorf("CreatedAt zone = %v, want UTC", got.CreatedAt.Location())
		}

		// A NULL timestamp must read back as invalid, not as a zero time.
		if got.DeletedAt.Valid {
			t.Errorf("DeletedAt = %+v, want invalid for a live row", got.DeletedAt)
		}

		if got.Version != 1 {
			t.Errorf("Version = %d, want 1", got.Version)
		}
	})
}

// Booleans are `boolean` on PostgreSQL and INTEGER on SQLite. This is the test
// that fails if dbtypes.Bool stops absorbing that difference.
func TestUserBooleanAndNullableTimeRoundTrip(t *testing.T) {
	t.Parallel()

	eachMigratedEngine(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		orgID := seedOrg(t, db)

		for _, active := range []bool{true, false} {
			userID := uuid.Must(uuid.NewV7())
			login := dbtypes.NewNullTime(time.Now().Add(-2 * time.Hour))

			var (
				gotActive dbtypes.Bool
				gotLogin  dbtypes.NullTime
			)

			if db.IsPostgres() {
				q := pg.New(db.DB)

				created, err := q.CreateUser(ctx, pg.CreateUserParams{
					ID: userID, OrgID: orgID, Email: uniqueEmail(active),
					IsActive: dbtypes.Bool(active), Locale: "en", Timezone: "UTC",
				})
				if err != nil {
					t.Fatalf("CreateUser: %v", err)
				}

				if _, loginErr := q.RecordUserLogin(ctx, pg.RecordUserLoginParams{
					ID: userID, OrgID: orgID, LastLoginAt: login,
				}); loginErr != nil {
					t.Fatalf("RecordUserLogin: %v", loginErr)
				}

				reread, rereadErr := q.GetUser(ctx, pg.GetUserParams{ID: userID, OrgID: orgID})
				if rereadErr != nil {
					t.Fatalf("GetUser: %v", rereadErr)
				}

				gotActive, gotLogin = reread.IsActive, reread.LastLoginAt
				_ = created
			} else {
				q := lite.New(db.DB)

				if _, err := q.CreateUser(ctx, lite.CreateUserParams{
					ID: userID, OrgID: orgID, Email: uniqueEmail(active),
					IsActive: dbtypes.Bool(active), Locale: "en", Timezone: "UTC",
				}); err != nil {
					t.Fatalf("CreateUser: %v", err)
				}

				if _, err := q.RecordUserLogin(ctx, lite.RecordUserLoginParams{
					ID: userID, OrgID: orgID, LastLoginAt: login,
				}); err != nil {
					t.Fatalf("RecordUserLogin: %v", err)
				}

				reread, err := q.GetUser(ctx, lite.GetUserParams{ID: userID, OrgID: orgID})
				if err != nil {
					t.Fatalf("GetUser: %v", err)
				}

				gotActive, gotLogin = reread.IsActive, reread.LastLoginAt
			}

			if gotActive.Bool() != active {
				t.Errorf("IsActive = %v, want %v on %s", gotActive.Bool(), active, db.Engine())
			}

			if !gotLogin.Valid {
				t.Fatalf("LastLoginAt is invalid after being set, on %s", db.Engine())
			}

			// Millisecond precision is the contract; anything coarser means a
			// format mismatch between what we write and what we parse.
			if diff := gotLogin.Time.Sub(login.Time.Time).Abs(); diff > time.Millisecond {
				t.Errorf("LastLoginAt drifted by %v on %s (wrote %v, read %v)",
					diff, db.Engine(), login.Time, gotLogin.Time)
			}
		}
	})
}

// Optimistic concurrency: a stale version must update zero rows rather than
// silently overwrite a newer one.
func TestUpdateWithStaleVersionAffectsNoRows(t *testing.T) {
	t.Parallel()

	eachMigratedEngine(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		orgID := seedOrg(t, db)
		now := dbtypes.Now()

		update := func(version int64) error {
			if db.IsPostgres() {
				_, err := pg.New(db.DB).UpdateOrganization(ctx, pg.UpdateOrganizationParams{
					ID: orgID, Name: "Renamed", Slug: "renamed",
					Settings: dbtypes.JSON("{}"), Plan: "free",
					UpdatedAt: now, Version: version,
				})

				return err
			}

			_, err := lite.New(db.DB).UpdateOrganization(ctx, lite.UpdateOrganizationParams{
				ID: orgID, Name: "Renamed", Slug: "renamed",
				Settings: dbtypes.JSON("{}"), Plan: "free",
				UpdatedAt: now, Version: version,
			})

			return err
		}

		// Version 1 is current, so this succeeds and bumps it to 2.
		if err := update(1); err != nil {
			t.Fatalf("update with current version failed on %s: %v", db.Engine(), err)
		}

		// Version 1 is now stale: the WHERE clause matches nothing, and a
		// :one query with no rows reports ErrNoRows.
		err := update(1)
		if err == nil {
			t.Fatalf("stale-version update succeeded on %s; it must affect no rows", db.Engine())
		}

		if !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("stale update error = %v, want sql.ErrNoRows", err)
		}
	})
}

// The upsert is the query sqlc mis-generated when a placeholder sat in the
// DO UPDATE clause, so it gets an explicit test on both engines.
func TestUserAttributeUpsert(t *testing.T) {
	t.Parallel()

	eachMigratedEngine(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		orgID := seedOrg(t, db)
		userID := seedUser(t, db, orgID)

		upsert := func(value, source string) (string, string, error) {
			now := dbtypes.Now()
			id := uuid.Must(uuid.NewV7())

			if db.IsPostgres() {
				row, err := pg.New(db.DB).UpsertUserAttribute(ctx, pg.UpsertUserAttributeParams{
					ID: id, OrgID: orgID, UserID: userID,
					Key: "region", Value: value, Source: source, UpdatedAt: now,
				})

				return row.Value, row.Source, err
			}

			row, err := lite.New(db.DB).UpsertUserAttribute(ctx, lite.UpsertUserAttributeParams{
				ID: id, OrgID: orgID, UserID: userID,
				Key: "region", Value: value, Source: source, UpdatedAt: now,
			})

			return row.Value, row.Source, err
		}

		value, source, err := upsert("EU", "manual")
		if err != nil {
			t.Fatalf("first upsert on %s: %v", db.Engine(), err)
		}

		if value != "EU" || source != "manual" {
			t.Errorf("first upsert = (%q, %q), want (EU, manual)", value, source)
		}

		// The conflicting write must update in place, carrying the new source.
		value, source, err = upsert("US", "oidc")
		if err != nil {
			t.Fatalf("conflicting upsert on %s: %v", db.Engine(), err)
		}

		if value != "US" || source != "oidc" {
			t.Errorf("second upsert = (%q, %q), want (US, oidc)", value, source)
		}

		// Exactly one row: an upsert that inserted twice would be worse than
		// one that failed.
		var attrs int
		if db.IsPostgres() {
			rows, lerr := pg.New(db.DB).ListUserAttributes(ctx,
				pg.ListUserAttributesParams{OrgID: orgID, UserID: userID})
			if lerr != nil {
				t.Fatalf("list: %v", lerr)
			}

			attrs = len(rows)
		} else {
			rows, lerr := lite.New(db.DB).ListUserAttributes(ctx,
				lite.ListUserAttributesParams{OrgID: orgID, UserID: userID})
			if lerr != nil {
				t.Fatalf("list: %v", lerr)
			}

			attrs = len(rows)
		}

		if attrs != 1 {
			t.Errorf("attribute count = %d, want 1", attrs)
		}
	})
}

// sqlc numbered the named limit and offset arguments in a different order per
// dialect. The generated SQL compensates, but only a real query proves it.
func TestPaginationBindsLimitAndOffsetCorrectly(t *testing.T) {
	t.Parallel()

	eachMigratedEngine(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()

		// Three organizations, ordered by name.
		for _, name := range []string{"aaa", "bbb", "ccc"} {
			createOrg(t, db, name)
		}

		list := func(limit, offset int64) []string {
			var names []string

			if db.IsPostgres() {
				rows, err := pg.New(db.DB).ListOrganizations(ctx,
					pg.ListOrganizationsParams{Limit: limit, Offset: offset})
				if err != nil {
					t.Fatalf("list: %v", err)
				}

				for _, r := range rows {
					names = append(names, r.Name)
				}

				return names
			}

			rows, err := lite.New(db.DB).ListOrganizations(ctx,
				lite.ListOrganizationsParams{Limit: limit, Offset: offset})
			if err != nil {
				t.Fatalf("list: %v", err)
			}

			for _, r := range rows {
				names = append(names, r.Name)
			}

			return names
		}

		// limit=1, offset=1 must return the second row. If the two were
		// swapped this would return the first row, or two rows.
		got := list(1, 1)
		if len(got) != 1 || got[0] != "bbb" {
			t.Errorf("list(limit=1, offset=1) on %s = %v, want [bbb]; "+
				"limit and offset may be bound in the wrong order", db.Engine(), got)
		}

		if got := list(2, 0); len(got) != 2 {
			t.Errorf("list(limit=2, offset=0) returned %d rows, want 2", len(got))
		}
	})
}

// --- seed helpers ---------------------------------------------------------

func createOrg(t *testing.T, db *store.DB, slug string) uuid.UUID {
	t.Helper()

	id := uuid.Must(uuid.NewV7())
	ctx := context.Background()

	var err error
	if db.IsPostgres() {
		_, err = pg.New(db.DB).CreateOrganization(ctx, pg.CreateOrganizationParams{
			ID: id, Name: slug, Slug: slug, Settings: dbtypes.JSON("{}"), Plan: "free",
		})
	} else {
		_, err = lite.New(db.DB).CreateOrganization(ctx, lite.CreateOrganizationParams{
			ID: id, Name: slug, Slug: slug, Settings: dbtypes.JSON("{}"), Plan: "free",
		})
	}

	if err != nil {
		t.Fatalf("seed organization %q on %s: %v", slug, db.Engine(), err)
	}

	return id
}

func seedOrg(t *testing.T, db *store.DB) uuid.UUID {
	t.Helper()

	return createOrg(t, db, "acme")
}

func seedUser(t *testing.T, db *store.DB, orgID uuid.UUID) uuid.UUID {
	t.Helper()

	id := uuid.Must(uuid.NewV7())
	ctx := context.Background()

	var err error
	if db.IsPostgres() {
		_, err = pg.New(db.DB).CreateUser(ctx, pg.CreateUserParams{
			ID: id, OrgID: orgID, Email: "seed@example.com",
			IsActive: true, Locale: "en", Timezone: "UTC",
		})
	} else {
		_, err = lite.New(db.DB).CreateUser(ctx, lite.CreateUserParams{
			ID: id, OrgID: orgID, Email: "seed@example.com",
			IsActive: true, Locale: "en", Timezone: "UTC",
		})
	}

	if err != nil {
		t.Fatalf("seed user on %s: %v", db.Engine(), err)
	}

	return id
}

func uniqueEmail(active bool) string {
	if active {
		return "active@example.com"
	}

	return "inactive@example.com"
}
