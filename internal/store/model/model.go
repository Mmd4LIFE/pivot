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

// IdentityProvider is an organization's configured SSO connection.
//
// ClientSecret is stored as written: envelope encryption is Part 15's, and it
// will cover this column and Phase 1's connection credentials together. The
// API never returns it.
type IdentityProvider struct {
	ID            uuid.UUID
	OrgID         uuid.UUID
	Slug          string
	Name          string
	Kind          string
	Issuer        string
	ClientID      string
	ClientSecret  string
	Scopes        string
	IsEnabled     dbtypes.Bool
	AutoProvision dbtypes.Bool
	DefaultRole   string
	LinkByEmail   dbtypes.Bool
	ClaimMapping  dbtypes.JSON
	CreatedAt     dbtypes.Time
	UpdatedAt     dbtypes.Time
	CreatedBy     uuid.NullUUID
	UpdatedBy     uuid.NullUUID
	DeletedAt     dbtypes.NullTime
	Version       int64
}

// Connection is a data source an organization can query.
//
// Password holds an envelope from internal/secrets, never plaintext, and the
// repository layer seals and opens it -- so above that layer this is the
// password and the fact that it is stored sealed is a property of storage.
//
// Host, Port, Database and Username are separate fields rather than one DSN
// because a DSN cannot be validated, redacted, partially shown, or edited one
// field at a time. Settings that do not generalize across sources live in
// Options.
//
// LastTestOk is meaningful only when LastTestedAt is valid: "never tested" is
// the absent timestamp, not a third state of the boolean.
type Connection struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	Slug        string
	Name        string
	Kind        string
	Description string

	Host     string
	Port     int64
	Database string
	Username string
	Password string
	SslMode  string
	Options  dbtypes.JSON

	MaxOpenConns        int64
	MaxRows             int64
	QueryTimeoutSeconds int64

	IsEnabled dbtypes.Bool

	LastTestedAt  dbtypes.NullTime
	LastTestOk    dbtypes.Bool
	LastTestError string

	CreatedAt dbtypes.Time
	UpdatedAt dbtypes.Time
	CreatedBy uuid.NullUUID
	UpdatedBy uuid.NullUUID
	DeletedAt dbtypes.NullTime
	Version   int64
}

// FederatedIdentity links an external subject to a Pivot user.
//
// The link is (provider, subject) and never the email address: an address can
// be reassigned inside a directory, and matching on it would hand the new
// holder the old one\'s account.
type FederatedIdentity struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	ProviderID  uuid.UUID
	UserID      uuid.UUID
	Subject     string
	CreatedAt   dbtypes.Time
	LastLoginAt dbtypes.NullTime
}

/*
CatalogTable is a table Pivot has seen in a connected database.

FirstSeenAt and LastSeenAt are what make a sync a comparison. Everything the
source reports gets a fresh LastSeenAt; anything left behind is what has gone.

RemovedAt is set rather than the row deleted. A table disappears for reasons
that are not "somebody dropped it" -- a permissions change, a migration caught
mid-flight -- and deleting would take the descriptions and the models pointing
at it along with it.
*/
type CatalogTable struct {
	ID           uuid.UUID
	OrgID        uuid.UUID
	ConnectionID uuid.UUID

	SchemaName string
	TableName  string
	TableType  string
	Comment    string

	FirstSeenAt dbtypes.Time
	LastSeenAt  dbtypes.Time
	RemovedAt   dbtypes.NullTime

	CreatedAt dbtypes.Time
	UpdatedAt dbtypes.Time
	Version   int64
}

/*
CatalogColumn is a column of one.

SourceType and CanonicalType are both kept. The first is what the database
called it; the second is what internal/datatype made of that. Keeping the
source spelling is what lets a stored catalog be re-normalized when Pivot
learns a mapping it did not have, without going back to the warehouse to ask
again.
*/
type CatalogColumn struct {
	ID      uuid.UUID
	OrgID   uuid.UUID
	TableID uuid.UUID

	ColumnName    string
	SourceType    string
	CanonicalType string

	IsNullable dbtypes.Bool
	Position   int64
	Comment    string

	FirstSeenAt dbtypes.Time
	LastSeenAt  dbtypes.Time
	RemovedAt   dbtypes.NullTime

	CreatedAt dbtypes.Time
	UpdatedAt dbtypes.Time
	Version   int64
}

/*
CatalogForeignKey is one column of one declared relationship.

A row per column with Ordinal giving the order, rather than a constraint
carrying two lists: no relational store holds ordered pairs without an array
type or a blob, and the ordering is the part that matters. Reading a source's
two column lists by joining rather than by position crosses them, and a join
built from that matches on columns never related to each other.
*/
type CatalogForeignKey struct {
	ID           uuid.UUID
	OrgID        uuid.UUID
	ConnectionID uuid.UUID

	ConstraintName string

	FromSchema string
	FromTable  string
	FromColumn string

	ToSchema string
	ToTable  string
	ToColumn string

	Ordinal int64

	FirstSeenAt dbtypes.Time
	LastSeenAt  dbtypes.Time
	RemovedAt   dbtypes.NullTime

	CreatedAt dbtypes.Time
	UpdatedAt dbtypes.Time
	Version   int64
}

/*
QueryLogEntry is one execution: what was run, by whom, and what happened.

Written when the query starts and completed when it finishes, so a row exists
while it is still running. That is what makes "what is running right now"
answerable, and it means a process that dies mid-query leaves the last thing
Pivot knew rather than nothing.

UserID is absent for work with no person behind it -- a scheduled refresh runs
as Pivot. Recording a fabricated user would make the log's audit value worse
than leaving the truth missing.
*/
type QueryLogEntry struct {
	ID           uuid.UUID
	OrgID        uuid.UUID
	ConnectionID uuid.UUID
	UserID       uuid.NullUUID

	SQLText string
	State   string

	StartedAt  dbtypes.Time
	FinishedAt dbtypes.NullTime
	DurationMs int64

	RowsReturned int64

	// BytesEstimated is the result's size in Pivot's memory, not on the wire:
	// the driver has decoded the rows before anything here can count them.
	BytesEstimated int64

	Truncated   dbtypes.Bool
	CacheStatus string

	ErrorMessage string
}
