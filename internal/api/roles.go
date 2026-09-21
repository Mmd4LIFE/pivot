package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

// RoleHandler serves the role catalog and role assignment.
type RoleHandler struct {
	repos   *repo.Repositories
	checker authz.Checker
	cache   *authz.Cache
	log     *slog.Logger
}

// NewRoleHandler builds the role endpoints.
//
// The cache is taken separately from the checker because granting a role has
// to invalidate it. Passing the same object twice would work today and break
// the moment the checker is wrapped in something else — which is exactly what
// adopting OpenFGA would do.
func NewRoleHandler(
	repos *repo.Repositories, checker authz.Checker, cache *authz.Cache, log *slog.Logger,
) *RoleHandler {
	return &RoleHandler{repos: repos, checker: checker, cache: cache, log: log}
}

// --- wire types ------------------------------------------------------------

type roleDescription struct {
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
}

type roleCatalogResponse struct {
	Roles []roleDescription `json:"roles"`
}

type roleAssignmentResponse struct {
	SubjectType string `json:"subjectType"`
	SubjectID   string `json:"subjectId"`
	Role        string `json:"role"`

	// GrantedAt is omitted rather than empty on the create response, where the
	// row has not been read back. An empty timestamp is worse than an absent
	// one: a client can check for absence, but "" parses as a zero date.
	GrantedAt string `json:"grantedAt,omitempty"`
}

type roleAssignmentListResponse struct {
	Assignments []roleAssignmentResponse `json:"assignments"`
}

type grantRoleRequest struct {
	SubjectType string `json:"subjectType"`
	SubjectID   string `json:"subjectId"`
	Role        string `json:"role"`
}

func (b *grantRoleRequest) Validate() []Detail {
	details := Collect(
		Required("subjectType", b.SubjectType),
		Required("subjectId", b.SubjectID),
		Required("role", b.Role),
	)

	if b.SubjectType != "" {
		if d := OneOf("subjectType", b.SubjectType,
			string(authz.SubjectUser), string(authz.SubjectGroup)); d != nil {
			details = append(details, *d)
		}
	}

	if b.Role != "" && !authz.IsBuiltinRole(authz.Relation(b.Role)) {
		details = append(details, Detail{
			Field:   "role",
			Message: "must be one of " + joinRoles(),
		})
	}

	if b.SubjectID != "" {
		if _, err := uuid.Parse(b.SubjectID); err != nil {
			details = append(details, Detail{Field: "subjectId", Message: "must be a UUID"})
		}
	}

	return details
}

// isBuiltinRoleName reports whether a string names a built-in role.
func isBuiltinRoleName(role string) bool {
	return authz.IsBuiltinRole(authz.Relation(role))
}

func joinRoles() string {
	out := ""

	for i, r := range authz.BuiltinRoles {
		if i > 0 {
			out += ", "
		}

		out += string(r)
	}

	return out
}

// --- GET /roles ------------------------------------------------------------

// handleCatalog lists the built-in roles and what each may do.
//
// Readable by any authenticated caller. Knowing that an "admin" role exists
// and can manage users is not sensitive — it is documentation — and a UI that
// offers a role picker needs it. Who *holds* a role is a different question,
// and that endpoint is gated.
func (h *RoleHandler) handleCatalog(w http.ResponseWriter, r *http.Request) {
	roles := make([]roleDescription, 0, len(authz.BuiltinRoles))

	for _, role := range authz.BuiltinRoles {
		perms := authz.PermissionsFor(role)

		names := make([]string, 0, len(perms))
		for _, p := range perms {
			names = append(names, string(p))
		}

		roles = append(roles, roleDescription{Role: string(role), Permissions: names})
	}

	WriteJSON(r.Context(), w, http.StatusOK, roleCatalogResponse{Roles: roles})
}

// --- GET /organization/role-assignments ------------------------------------

// handleList returns who holds what in the caller's organization.
func (h *RoleHandler) handleList(w http.ResponseWriter, r *http.Request) {
	scope, err := tenant.FromContext(r.Context())
	if err != nil {
		WriteError(w, r, err)

		return
	}

	rows, err := h.repos.Roles.ListOnObject(r.Context(),
		string(authz.TypeOrganization), scope.OrgID())
	if err != nil {
		WriteError(w, r, err)

		return
	}

	out := make([]roleAssignmentResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, roleAssignmentResponse{
			SubjectType: row.SubjectType,
			SubjectID:   row.SubjectID.String(),
			Role:        row.Relation,
			GrantedAt:   row.CreatedAt.Time.UTC().Format("2006-01-02T15:04:05.999Z"),
		})
	}

	WriteJSON(r.Context(), w, http.StatusOK, roleAssignmentListResponse{Assignments: out})
}

// --- POST /organization/role-assignments -----------------------------------

// handleGrant assigns a role.
func (h *RoleHandler) handleGrant(w http.ResponseWriter, r *http.Request) {
	var body grantRoleRequest
	if err := Decode(w, r, &body); err != nil {
		WriteError(w, r, err)

		return
	}

	scope, err := tenant.FromContext(r.Context())
	if err != nil {
		WriteError(w, r, err)

		return
	}

	subjectID, err := uuid.Parse(body.SubjectID)
	if err != nil {
		WriteError(w, r, ValidationError(Detail{Field: "subjectId", Message: "must be a UUID"}))

		return
	}

	grant := repo.GrantRole{
		SubjectType: body.SubjectType,
		SubjectID:   subjectID,
		Relation:    body.Role,
		ObjectType:  string(authz.TypeOrganization),
		ObjectID:    scope.OrgID(),
	}

	// A group grant is a userset: "every member of this group".
	if body.SubjectType == string(authz.SubjectGroup) {
		grant.SubjectRelation = string(authz.RelationMember)
	}

	if gerr := h.repos.Roles.Grant(r.Context(), grant); gerr != nil {
		if errors.Is(gerr, repo.ErrNotFound) {
			// The subject does not exist in this organization. Reported as a
			// validation failure on the field rather than a bare 404, because
			// the request as a whole is findable — one value in it is not.
			WriteError(w, r, ValidationError(Detail{
				Field:   "subjectId",
				Message: "no such " + body.SubjectType + " in this organization",
			}))

			return
		}

		WriteError(w, r, gerr)

		return
	}

	// The decision cache holds denials that this grant has just made wrong.
	h.invalidate()

	WriteJSON(r.Context(), w, http.StatusCreated, roleAssignmentResponse{
		SubjectType: grant.SubjectType,
		SubjectID:   grant.SubjectID.String(),
		Role:        grant.Relation,
	})
}

// --- DELETE /organization/role-assignments/{subjectType}/{subjectId}/{role} -

// handleRevoke removes a role.
func (h *RoleHandler) handleRevoke(w http.ResponseWriter, r *http.Request) {
	scope, err := tenant.FromContext(r.Context())
	if err != nil {
		WriteError(w, r, err)

		return
	}

	subjectType := r.PathValue("subjectType")
	role := r.PathValue("role")

	subjectID, err := uuid.Parse(r.PathValue("subjectId"))
	if err != nil {
		WriteError(w, r, ValidationError(Detail{Field: "subjectId", Message: "must be a UUID"}))

		return
	}

	if subjectType != string(authz.SubjectUser) && subjectType != string(authz.SubjectGroup) {
		WriteError(w, r, ValidationError(Detail{
			Field: "subjectType", Message: "must be user or group",
		}))

		return
	}

	// Refuse to remove the last administrator.
	//
	// An organization with nobody who can grant roles is recoverable only from
	// the command line, and the people most likely to reach that state are the
	// least likely to have shell access. The check counts assignments rather
	// than people, so it stops the common single-admin case and not every
	// conceivable one — a group grant covering many admins counts as one.
	if authz.Relation(role) == authz.RelationAdmin {
		holders, cerr := h.repos.Roles.CountHolders(r.Context(),
			role, string(authz.TypeOrganization), scope.OrgID())
		if cerr != nil {
			WriteError(w, r, cerr)

			return
		}

		if holders <= 1 {
			WriteError(w, r, &APIError{
				Code: CodeValidationFailed,
				Message: "This is the last administrator. " +
					"Grant the role to someone else before removing it.",
			})

			return
		}
	}

	revoke := repo.GrantRole{
		SubjectType: subjectType,
		SubjectID:   subjectID,
		Relation:    role,
		ObjectType:  string(authz.TypeOrganization),
		ObjectID:    scope.OrgID(),
	}

	if subjectType == string(authz.SubjectGroup) {
		revoke.SubjectRelation = string(authz.RelationMember)
	}

	if rerr := h.repos.Roles.Revoke(r.Context(), revoke); rerr != nil {
		WriteError(w, r, rerr)

		return
	}

	h.invalidate()

	w.WriteHeader(http.StatusNoContent)
}

// --- DELETE /admin/sessions/{id} -------------------------------------------

// handleRevokeAnySession ends any session in the caller's organization.
//
// This is the administrative counterpart to the self-service revoke in Part
// 6-b, and the reason `SessionRepo.Revoke` existed without a caller until now:
// it is scoped to the organization rather than to one user, which is the right
// power for an administrator responding to a compromise and the wrong one for
// somebody managing their own devices.
func (h *RoleHandler) handleRevokeAnySession(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, r, ValidationError(Detail{Field: "id", Message: "must be a UUID"}))

		return
	}

	if rerr := h.repos.Sessions.Revoke(r.Context(), sessionID); rerr != nil {
		WriteError(w, r, rerr)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// invalidate drops cached decisions after a relationship write.
func (h *RoleHandler) invalidate() {
	if h.cache != nil {
		h.cache.Invalidate()
	}
}
