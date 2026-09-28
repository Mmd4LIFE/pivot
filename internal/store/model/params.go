package model

import (
	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store/dbtypes"
)

// Query parameters, mirroring the generated structs field for field so the
// adapters convert with a single expression.
//
// Field ORDER matters here, not just names and types: Go permits conversion
// between struct types only when their fields correspond in order. The
// compile-time assertions in the repository package are what catch a mismatch.

// --- organizations --------------------------------------------------------

type CreateOrganizationParams struct {
	ID        uuid.UUID
	Name      string
	Slug      string
	Settings  dbtypes.JSON
	Plan      string
	CreatedBy uuid.NullUUID
	UpdatedBy uuid.NullUUID
}

type ListOrganizationsParams struct {
	Offset int64
	Limit  int64
}

type UpdateOrganizationParams struct {
	Name      string
	Slug      string
	Settings  dbtypes.JSON
	Plan      string
	UpdatedBy uuid.NullUUID
	UpdatedAt dbtypes.Time
	ID        uuid.UUID
	Version   int64
}

type SoftDeleteOrganizationParams struct {
	DeletedAt dbtypes.NullTime
	UpdatedBy uuid.NullUUID
	ID        uuid.UUID
}

// --- users ----------------------------------------------------------------

type CreateUserParams struct {
	ID           uuid.UUID
	OrgID        uuid.UUID
	Email        string
	Name         string
	AvatarURL    NullString
	PasswordHash NullString
	IsActive     dbtypes.Bool
	Locale       string
	Timezone     string
	CreatedBy    uuid.NullUUID
	UpdatedBy    uuid.NullUUID
}

type GetUserParams struct {
	ID    uuid.UUID
	OrgID uuid.UUID
}

type GetUserByEmailParams struct {
	OrgID uuid.UUID
	Email string
}

type ListUsersParams struct {
	OrgID  uuid.UUID
	Offset int64
	Limit  int64
}

type UpdateUserParams struct {
	Email     string
	Name      string
	AvatarURL NullString
	IsActive  dbtypes.Bool
	Locale    string
	Timezone  string
	UpdatedBy uuid.NullUUID
	UpdatedAt dbtypes.Time
	ID        uuid.UUID
	OrgID     uuid.UUID
	Version   int64
}

type UpdateUserPasswordParams struct {
	PasswordHash NullString
	UpdatedBy    uuid.NullUUID
	UpdatedAt    dbtypes.Time
	ID           uuid.UUID
	OrgID        uuid.UUID
}

type RecordUserLoginParams struct {
	LastLoginAt dbtypes.NullTime
	ID          uuid.UUID
	OrgID       uuid.UUID
}

type SoftDeleteUserParams struct {
	DeletedAt dbtypes.NullTime
	UpdatedBy uuid.NullUUID
	ID        uuid.UUID
	OrgID     uuid.UUID
}

// --- groups ---------------------------------------------------------------

type CreateGroupParams struct {
	ID            uuid.UUID
	OrgID         uuid.UUID
	Name          string
	Description   string
	ParentGroupID uuid.NullUUID
	ExternalID    NullString
	CreatedBy     uuid.NullUUID
	UpdatedBy     uuid.NullUUID
}

type GetGroupParams struct {
	ID    uuid.UUID
	OrgID uuid.UUID
}

type GetGroupByNameParams struct {
	OrgID uuid.UUID
	Name  string
}

type ListGroupsParams struct {
	OrgID  uuid.UUID
	Offset int64
	Limit  int64
}

type ListChildGroupsParams struct {
	OrgID         uuid.UUID
	ParentGroupID uuid.NullUUID
}

type UpdateGroupParams struct {
	Name          string
	Description   string
	ParentGroupID uuid.NullUUID
	ExternalID    NullString
	UpdatedBy     uuid.NullUUID
	UpdatedAt     dbtypes.Time
	ID            uuid.UUID
	OrgID         uuid.UUID
	Version       int64
}

type SoftDeleteGroupParams struct {
	DeletedAt dbtypes.NullTime
	UpdatedBy uuid.NullUUID
	ID        uuid.UUID
	OrgID     uuid.UUID
}

// --- group membership -----------------------------------------------------

type AddGroupMemberParams struct {
	OrgID   uuid.UUID
	GroupID uuid.UUID
	UserID  uuid.UUID
	AddedBy uuid.NullUUID
}

type RemoveGroupMemberParams struct {
	OrgID   uuid.UUID
	GroupID uuid.UUID
	UserID  uuid.UUID
}

type ListGroupMembersParams struct {
	OrgID   uuid.UUID
	GroupID uuid.UUID
}

type ListUserGroupsParams struct {
	OrgID  uuid.UUID
	UserID uuid.UUID
}

type IsGroupMemberParams struct {
	OrgID   uuid.UUID
	GroupID uuid.UUID
	UserID  uuid.UUID
}

// --- user attributes ------------------------------------------------------

type UpsertUserAttributeParams struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	UserID    uuid.UUID
	Key       string
	Value     string
	Source    string
	UpdatedAt dbtypes.Time
}

type GetUserAttributeParams struct {
	OrgID  uuid.UUID
	UserID uuid.UUID
	Key    string
}

type ListUserAttributesParams struct {
	OrgID  uuid.UUID
	UserID uuid.UUID
}

type DeleteUserAttributeParams struct {
	OrgID  uuid.UUID
	UserID uuid.UUID
	Key    string
}

type DeleteUserAttributesBySourceParams struct {
	OrgID  uuid.UUID
	UserID uuid.UUID
	Source string
}

// --- sessions -------------------------------------------------------------

type CreateSessionParams struct {
	ID                uuid.UUID
	OrgID             uuid.UUID
	UserID            uuid.UUID
	TokenHash         string
	ExpiresAt         dbtypes.Time
	AbsoluteExpiresAt dbtypes.Time
	IP                string
	UserAgent         string
}

type TouchSessionParams struct {
	LastSeenAt dbtypes.Time
	ExpiresAt  dbtypes.Time
	ID         uuid.UUID
}

type RevokeSessionParams struct {
	RevokedAt dbtypes.NullTime
	ID        uuid.UUID
	OrgID     uuid.UUID
}

type RevokeSessionForUserParams struct {
	RevokedAt dbtypes.NullTime
	ID        uuid.UUID
	UserID    uuid.UUID
	OrgID     uuid.UUID
}

type RevokeUserSessionsParams struct {
	RevokedAt dbtypes.NullTime
	UserID    uuid.UUID
	OrgID     uuid.UUID
}

// RevokeOtherUserSessionsParams keeps ID and revokes the rest.
type RevokeOtherUserSessionsParams struct {
	RevokedAt dbtypes.NullTime
	UserID    uuid.UUID
	OrgID     uuid.UUID

	// ID is the session to keep, not the one to revoke. Named as the query
	// names it; read the comment above before changing either.
	ID uuid.UUID
}

type ListUserSessionsParams struct {
	UserID uuid.UUID
	OrgID  uuid.UUID
}

type DeleteExpiredSessionsParams struct {
	AbsoluteExpiresAt dbtypes.Time
	RevokedAt         dbtypes.NullTime
}

// --- login attempts -------------------------------------------------------

type GetLoginAttemptParams struct {
	OrgID uuid.UUID
	Email string
}

type RecordFailedLoginParams struct {
	ID            uuid.UUID
	OrgID         uuid.UUID
	Email         string
	FirstFailedAt dbtypes.Time
	LastFailedAt  dbtypes.Time
	LockedUntil   dbtypes.NullTime
}

type SetLoginLockParams struct {
	LockedUntil dbtypes.NullTime
	OrgID       uuid.UUID
	Email       string
}

type ClearLoginAttemptsParams struct {
	OrgID uuid.UUID
	Email string
}

type DeleteStaleLoginAttemptsParams struct {
	LastFailedAt dbtypes.Time
	LockedUntil  dbtypes.NullTime
}

// --- role assignments -----------------------------------------------------

type ListObjectGrantsParams struct {
	OrgID      uuid.UUID
	ObjectType string
	ObjectID   uuid.UUID
	SubjectID  uuid.UUID
}

type ListSubjectGrantsParams struct {
	OrgID       uuid.UUID
	SubjectType string
	SubjectID   uuid.UUID
}

type ListGrantsOnObjectParams struct {
	OrgID      uuid.UUID
	ObjectType string
	ObjectID   uuid.UUID
}

type GrantRoleParams struct {
	ID              uuid.UUID
	OrgID           uuid.UUID
	SubjectType     string
	SubjectID       uuid.UUID
	SubjectRelation string
	Relation        string
	ObjectType      string
	ObjectID        uuid.UUID
	CreatedBy       uuid.NullUUID
}

type RevokeRoleParams struct {
	OrgID           uuid.UUID
	SubjectType     string
	SubjectID       uuid.UUID
	SubjectRelation string
	Relation        string
	ObjectType      string
	ObjectID        uuid.UUID
}

type RevokeAllForSubjectParams struct {
	OrgID       uuid.UUID
	SubjectType string
	SubjectID   uuid.UUID
}

type CountRoleHoldersParams struct {
	OrgID      uuid.UUID
	Relation   string
	ObjectType string
	ObjectID   uuid.UUID
}

// --- identity providers ---------------------------------------------------

type CreateIdentityProviderParams struct {
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
	LinkByEmail   dbtypes.Bool
	DefaultRole   string
	ClaimMapping  dbtypes.JSON
	CreatedBy     uuid.NullUUID
	UpdatedBy     uuid.NullUUID
}

type GetIdentityProviderParams struct {
	ID    uuid.UUID
	OrgID uuid.UUID
}

type GetIdentityProviderBySlugParams struct {
	OrgID uuid.UUID
	Slug  string
}

type UpdateIdentityProviderParams struct {
	Slug          string
	Name          string
	Issuer        string
	ClientID      string
	ClientSecret  string
	Scopes        string
	IsEnabled     dbtypes.Bool
	AutoProvision dbtypes.Bool
	LinkByEmail   dbtypes.Bool
	DefaultRole   string
	ClaimMapping  dbtypes.JSON
	UpdatedBy     uuid.NullUUID
	UpdatedAt     dbtypes.Time
	ID            uuid.UUID
	OrgID         uuid.UUID
	Version       int64
}

type SoftDeleteIdentityProviderParams struct {
	DeletedAt dbtypes.NullTime
	UpdatedBy uuid.NullUUID
	ID        uuid.UUID
	OrgID     uuid.UUID
}

type GetFederatedIdentityParams struct {
	ProviderID uuid.UUID
	Subject    string
}

type ListFederatedIdentitiesForUserParams struct {
	OrgID  uuid.UUID
	UserID uuid.UUID
}

type LinkFederatedIdentityParams struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	ProviderID  uuid.UUID
	UserID      uuid.UUID
	Subject     string
	LastLoginAt dbtypes.NullTime
}

type RecordFederatedLoginParams struct {
	LastLoginAt dbtypes.NullTime
	ProviderID  uuid.UUID
	Subject     string
}

// --- connections -----------------------------------------------------------
//
// Field order mirrors the generated structs exactly: the conversions in
// repo/adapter.go are direct, and a differing order breaks them - the lesson
// of Part 4-a.

type CreateConnectionParams struct {
	ID                  uuid.UUID
	OrgID               uuid.UUID
	Slug                string
	Name                string
	Kind                string
	Description         string
	Host                string
	Port                int64
	Database            string
	Username            string
	Password            string
	SslMode             string
	Options             dbtypes.JSON
	MaxOpenConns        int64
	MaxRows             int64
	QueryTimeoutSeconds int64
	IsEnabled           dbtypes.Bool
	CreatedBy           uuid.NullUUID
	UpdatedBy           uuid.NullUUID
}

type GetConnectionParams struct {
	ID    uuid.UUID
	OrgID uuid.UUID
}

type GetConnectionBySlugParams struct {
	OrgID uuid.UUID
	Slug  string
}

type UpdateConnectionParams struct {
	Slug                string
	Name                string
	Description         string
	Host                string
	Port                int64
	Database            string
	Username            string
	Password            string
	SslMode             string
	Options             dbtypes.JSON
	MaxOpenConns        int64
	MaxRows             int64
	QueryTimeoutSeconds int64
	IsEnabled           dbtypes.Bool
	UpdatedBy           uuid.NullUUID
	UpdatedAt           dbtypes.Time
	ID                  uuid.UUID
	OrgID               uuid.UUID
	Version             int64
}

type RecordConnectionTestParams struct {
	LastTestedAt  dbtypes.NullTime
	LastTestOk    dbtypes.Bool
	LastTestError string
	ID            uuid.UUID
	OrgID         uuid.UUID
}

type SoftDeleteConnectionParams struct {
	DeletedAt dbtypes.NullTime
	UpdatedBy uuid.NullUUID
	ID        uuid.UUID
	OrgID     uuid.UUID
}

// --- catalog -----------------------------------------------------------------

type UpsertCatalogTableParams struct {
	ID           uuid.UUID
	OrgID        uuid.UUID
	ConnectionID uuid.UUID
	SchemaName   string
	TableName    string
	TableType    string
	Comment      string
	FirstSeenAt  dbtypes.Time
	LastSeenAt   dbtypes.Time
}

type UpsertCatalogColumnParams struct {
	ID            uuid.UUID
	OrgID         uuid.UUID
	TableID       uuid.UUID
	ColumnName    string
	SourceType    string
	CanonicalType string
	IsNullable    dbtypes.Bool
	Position      int64
	Comment       string
	FirstSeenAt   dbtypes.Time
	LastSeenAt    dbtypes.Time
}

// SweepCatalogTablesParams marks everything not seen by a sync as gone.
//
// LastSeenAt is the cutoff rather than the value: rows whose last_seen_at is
// older than this were not touched by the sync that just ran.
type SweepCatalogTablesParams struct {
	RemovedAt    dbtypes.NullTime
	UpdatedAt    dbtypes.Time
	ConnectionID uuid.UUID
	OrgID        uuid.UUID
	LastSeenAt   dbtypes.Time
}

type SweepCatalogColumnsParams struct {
	RemovedAt    dbtypes.NullTime
	UpdatedAt    dbtypes.Time
	OrgID        uuid.UUID
	LastSeenAt   dbtypes.Time
	ConnectionID uuid.UUID
}

type ListCatalogTablesParams struct {
	ConnectionID uuid.UUID
	OrgID        uuid.UUID
}

type ListCatalogColumnsParams struct {
	ConnectionID uuid.UUID
	OrgID        uuid.UUID
}

type ListCatalogColumnsForTableParams struct {
	TableID uuid.UUID
	OrgID   uuid.UUID
}

type GetCatalogTableParams struct {
	ConnectionID uuid.UUID
	OrgID        uuid.UUID
	SchemaName   string
	TableName    string
}

type UpsertCatalogForeignKeyParams struct {
	ID             uuid.UUID
	OrgID          uuid.UUID
	ConnectionID   uuid.UUID
	ConstraintName string
	FromSchema     string
	FromTable      string
	FromColumn     string
	ToSchema       string
	ToTable        string
	ToColumn       string
	Ordinal        int64
	FirstSeenAt    dbtypes.Time
	LastSeenAt     dbtypes.Time
}

type SweepCatalogForeignKeysParams struct {
	RemovedAt    dbtypes.NullTime
	UpdatedAt    dbtypes.Time
	ConnectionID uuid.UUID
	OrgID        uuid.UUID
	LastSeenAt   dbtypes.Time
}

type ListCatalogForeignKeysParams struct {
	ConnectionID uuid.UUID
	OrgID        uuid.UUID
}

// --- the query log -----------------------------------------------------------

type StartQueryLogParams struct {
	ID           uuid.UUID
	OrgID        uuid.UUID
	ConnectionID uuid.UUID
	UserID       uuid.NullUUID
	SQLText      string
	StartedAt    dbtypes.Time
}

type FinishQueryLogParams struct {
	State          string
	FinishedAt     dbtypes.NullTime
	DurationMs     int64
	RowsReturned   int64
	BytesEstimated int64
	Truncated      dbtypes.Bool
	CacheStatus    string
	ErrorMessage   string
	ID             uuid.UUID
	OrgID          uuid.UUID
}

type ListQueryLogParams struct {
	OrgID uuid.UUID
	Limit int64
}

type GetQueryLogEntryParams struct {
	ID    uuid.UUID
	OrgID uuid.UUID
}
