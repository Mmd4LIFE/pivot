// Package model holds the domain types the repository layer speaks in.
//
// They are declared here rather than aliased to one engine's generated
// structs. sqlc emits structurally identical types for PostgreSQL and SQLite
// (see sqlc.yaml), so conversion between them and these is a single
// expression — but aliasing would make the domain layer structurally depend on
// one engine's generated code, and "which engine is canonical?" is not a
// question the domain should have an answer to.
//
// The conversions are asserted at compile time in the repository package, so a
// field added to the schema without a matching field here fails the build
// rather than drifting.
package model

import (
	"database/sql"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store/dbtypes"
)

// Organization is a tenant.
type Organization struct {
	ID        uuid.UUID
	Name      string
	Slug      string
	Settings  dbtypes.JSON
	Plan      string
	CreatedAt dbtypes.Time
	UpdatedAt dbtypes.Time
	CreatedBy uuid.NullUUID
	UpdatedBy uuid.NullUUID
	DeletedAt dbtypes.NullTime
	Version   int64
}

// User belongs to exactly one organization.
type User struct {
	ID           uuid.UUID
	OrgID        uuid.UUID
	Email        string
	Name         string
	AvatarURL    NullString
	PasswordHash NullString
	IsActive     dbtypes.Bool
	LastLoginAt  dbtypes.NullTime
	Locale       string
	Timezone     string
	CreatedAt    dbtypes.Time
	UpdatedAt    dbtypes.Time
	CreatedBy    uuid.NullUUID
	UpdatedBy    uuid.NullUUID
	DeletedAt    dbtypes.NullTime
	Version      int64
}

// Group is a named set of users, optionally nested.
type Group struct {
	ID            uuid.UUID
	OrgID         uuid.UUID
	Name          string
	Description   string
	ParentGroupID uuid.NullUUID
	ExternalID    NullString
	CreatedAt     dbtypes.Time
	UpdatedAt     dbtypes.Time
	CreatedBy     uuid.NullUUID
	UpdatedBy     uuid.NullUUID
	DeletedAt     dbtypes.NullTime
	Version       int64
}

// UserAttribute is a key/value pair on a user, with provenance. These feed
// row-level security in Phase 4.
type UserAttribute struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	UserID    uuid.UUID
	Key       string
	Value     string
	Source    string
	CreatedAt dbtypes.Time
	UpdatedAt dbtypes.Time
}

// GroupMember links a user to a group.
type GroupMember struct {
	OrgID   uuid.UUID
	GroupID uuid.UUID
	UserID  uuid.UUID
	AddedAt dbtypes.Time
	AddedBy uuid.NullUUID
}

// NullString is a nullable string column.
//
// It is an ALIAS for [sql.NullString], not a distinct type, and that matters:
// Go permits conversion between struct types only when their fields are
// *identical*, not merely convertible. A separate `type NullString struct{...}`
// would look the same and make every model/generated conversion fail to
// compile. The alias keeps the conversions single expressions while still
// letting callers spell it `model.NullString` without importing database/sql.
type NullString = sql.NullString

// NewNullString wraps a non-empty string as valid. The empty string is treated
// as NULL, which is what every caller here means by it.
func NewNullString(s string) NullString {
	if s == "" {
		return NullString{}
	}

	return NullString{String: s, Valid: true}
}

// StringOr returns the string, or fallback when null.
//
// A function rather than a method because Go does not allow methods on an
// alias to a type from another package.
func StringOr(n NullString, fallback string) string {
	if !n.Valid {
		return fallback
	}

	return n.String
}

// AttributeSource values allowed by the user_attributes CHECK constraint.
const (
	SourceManual = "manual"
	SourceOIDC   = "oidc"
	SourceSAML   = "saml"
	SourceSCIM   = "scim"
)

// Session is a server-side login session.
//
// The token itself is never stored — only TokenHash — so a database dump does
// not hand the reader a set of working sessions.
type Session struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	UserID    uuid.UUID
	TokenHash string
	IssuedAt  dbtypes.Time

	// ExpiresAt is the idle timeout and moves forward on use.
	ExpiresAt dbtypes.Time

	// AbsoluteExpiresAt is a hard cap and is never extended, so an active
	// session still ends eventually.
	AbsoluteExpiresAt dbtypes.Time

	LastSeenAt dbtypes.Time
	IP         string
	UserAgent  string
	RevokedAt  dbtypes.NullTime
}

// LoginAttempt tracks consecutive failures for progressive lockout.
//
// Keyed by the attempted email rather than a user id, because a row must exist
// even when the account does not — locking out only real accounts would turn
// the lockout into a user-enumeration oracle.
type LoginAttempt struct {
	ID            uuid.UUID
	OrgID         uuid.UUID
	Email         string
	FailedCount   int64
	FirstFailedAt dbtypes.Time
	LastFailedAt  dbtypes.Time
	LockedUntil   dbtypes.NullTime
}

// RoleAssignment is one stored relationship, in Zanzibar's shape: a subject
// holds a relation on an object.
//
// SubjectRelation empty means the subject is a single user. Non-empty makes it
// a userset — "member" names every member of the group — which is how a role
// is granted to a group rather than one person at a time.
//
// Group membership deliberately lives in group_members rather than here: one
// fact, one place. See migration 00004 and ADR-0009's amendment.
type RoleAssignment struct {
	ID              uuid.UUID
	OrgID           uuid.UUID
	SubjectType     string
	SubjectID       uuid.UUID
	SubjectRelation string
	Relation        string
	ObjectType      string
	ObjectID        uuid.UUID
	CreatedAt       dbtypes.Time
	CreatedBy       uuid.NullUUID
}
