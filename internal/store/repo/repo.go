package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/dbtypes"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

// Errors the repository layer returns. Callers distinguish them with
// errors.Is rather than by inspecting driver errors, so the HTTP layer in
// Part 5 can map them to status codes without knowing about SQL.
var (
	// ErrNotFound means no row matched within the caller's tenant. It is
	// deliberately indistinguishable from "exists, but belongs to another
	// tenant": reporting the difference would leak the existence of other
	// tenants' records.
	ErrNotFound = errors.New("repo: not found")

	// ErrConflict means an optimistic-concurrency check failed: the row was
	// modified since the caller read it.
	ErrConflict = errors.New("repo: version conflict")

	// ErrDuplicate means a uniqueness constraint rejected the write.
	ErrDuplicate = errors.New("repo: already exists")
)

// Repositories is the full data access surface, constructed once per database.
// Every scoped repository must be registered here. The reflection test in
// isolation_test.go walks these fields, so a repository added to the struct is
// covered by the unscoped-context check automatically — and one that is not
// registered is not covered.
type Repositories struct {
	Organizations  *OrganizationRepo
	Users          *UserRepo
	Groups         *GroupRepo
	UserAttributes *UserAttributeRepo
	Sessions       *SessionRepo
	Roles          *RoleRepo

	q      Querier
	events *EventBus
}

// New builds the repositories over a database.
func New(db *store.DB) *Repositories {
	return NewWithQuerier(NewQuerier(db))
}

// NewWithQuerier builds the repositories over an arbitrary Querier, for tests
// that substitute a fake.
func NewWithQuerier(q Querier) *Repositories {
	events := NewEventBus()

	b := base{q: q, events: events}

	return &Repositories{
		q:              q,
		events:         events,
		Organizations:  &OrganizationRepo{base: b},
		Users:          &UserRepo{base: b},
		Groups:         &GroupRepo{base: b},
		UserAttributes: &UserAttributeRepo{base: b},
		Sessions:       &SessionRepo{base: b},
		Roles:          &RoleRepo{base: b},
	}
}

// Events returns the change event bus. Part 4-b's audit log and the search
// index in Phase 1 subscribe to it.
func (r *Repositories) Events() *EventBus { return r.events }

// base carries what every repository needs, and — more importantly — the
// scope() helper that every method must call before touching the database.
type base struct {
	q      Querier
	events *EventBus
}

// scope extracts the tenant from the context.
//
// This is the single choke point for tenant isolation. Every repository method
// starts here, and a context with no scope produces an error before any SQL
// runs. There is no variant that defaults to "all tenants", because that is
// precisely the behavior this layer exists to make unreachable.
func (b base) scope(ctx context.Context) (tenant.Scope, error) {
	s, err := tenant.FromContext(ctx)
	if err != nil {
		return tenant.Scope{}, fmt.Errorf("repo: %w", err)
	}

	return s, nil
}

// now returns the current timestamp, normalized for storage.
//
// Timestamps are set here rather than left to column defaults so that a row's
// updated_at is the moment the application decided to write, and so the value
// is identical on both engines.
func (b base) now() dbtypes.Time { return dbtypes.Now() }

// translate maps a driver error onto this package's sentinel errors.
func translate(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}

	if isUniqueViolation(err) {
		return ErrDuplicate
	}

	return err
}

// isUniqueViolation reports whether err is a uniqueness constraint failure.
//
// The two drivers report this differently and neither exposes a portable
// sentinel, so this matches on the message. It is checked by tests on both
// engines, which is what keeps a string match honest.
func isUniqueViolation(err error) bool {
	msg := err.Error()

	for _, marker := range []string{
		"SQLSTATE 23505",      // PostgreSQL unique_violation
		"duplicate key value", // PostgreSQL, text form
		"UNIQUE constraint failed",
		"constraint failed: UNIQUE",
	} {
		if containsFold(msg, marker) {
			return true
		}
	}

	return false
}

func containsFold(haystack, needle string) bool {
	if len(needle) > len(haystack) {
		return false
	}

	for i := 0; i+len(needle) <= len(haystack); i++ {
		if equalFold(haystack[i:i+len(needle)], needle) {
			return true
		}
	}

	return false
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range len(a) {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}

		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}

		if ca != cb {
			return false
		}
	}

	return true
}

// affectedOrNotFound is the unversioned counterpart: zero rows means the row
// is not there, or not ours.
func affectedOrNotFound(n int64, err error) error {
	if err != nil {
		return translate(err)
	}

	if n == 0 {
		return ErrNotFound
	}

	return nil
}

// newID returns a fresh UUID v7. Time-sortable, so it indexes well and rows
// created together stay together on disk.
func newID() uuid.UUID { return uuid.Must(uuid.NewV7()) }
