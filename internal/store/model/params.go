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
