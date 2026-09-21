package authz

import (
	"fmt"
	"strings"
)

// The vocabulary here is Zanzibar's, and deliberately so: a relationship is a
// (subject, relation, object) triple, exactly what OpenFGA stores and what its
// Write API accepts. ADR-0009 chooses OpenFGA for the model; keeping the same
// shape means the eventual swap is a data export and an implementation change
// behind [Checker], not a redesign.
//
// See docs/architecture/security-model.md#3-authorization for the full model.
// Phase 0 implements the organization, group and user layer; collections,
// dashboards and semantic models arrive with Phase 4, which is where the
// recursion that justifies a relationship model actually appears.

// ObjectType is the kind of thing a permission is checked against.
type ObjectType string

const (
	// TypeOrganization is the tenant itself. Every Phase 0 permission is
	// checked against one.
	TypeOrganization ObjectType = "organization"

	// TypeGroup is a named set of users.
	TypeGroup ObjectType = "group"
)

// SubjectType is the kind of thing that holds a relation.
type SubjectType string

const (
	// SubjectUser is a single user.
	SubjectUser SubjectType = "user"

	// SubjectGroup names a group. Combined with a relation it is a Zanzibar
	// *userset*: "group:analysts#member" means every member of that group,
	// which is what lets a role be granted to a group rather than one by one.
	SubjectGroup SubjectType = "group"
)

// Relation is a stored edge in the graph — what is actually written down.
//
// Relations are distinct from [Permission]: a relation is an assignment, a
// permission is a question. "ada is an admin of acme" is a relation; "may ada
// manage users" is a permission, answered by walking relations.
type Relation string

// The relations Phase 0 stores.
const (
	// RelationMember links a user to a group.
	RelationMember Relation = "member"

	// The four built-in roles, held on an organization.
	RelationAdmin   Relation = "admin"
	RelationEditor  Relation = "editor"
	RelationAnalyst Relation = "analyst"
	RelationViewer  Relation = "viewer"
)

// Role is a built-in bundle of permissions.
//
// Roles are a convenience over relations, not a separate concept: each role is
// the relation of the same name. Phase 4 adds per-object grants alongside them,
// which is why permissions are resolved through the graph even now, when a
// lookup table would give the same answers.
type Role = Relation

// BuiltinRoles are the roles an organization starts with, most privileged
// first. The order is used for display and for picking the strongest role a
// user holds.
var BuiltinRoles = []Role{RelationAdmin, RelationEditor, RelationAnalyst, RelationViewer}

// IsBuiltinRole reports whether r names a built-in role.
func IsBuiltinRole(r Relation) bool {
	for _, role := range BuiltinRoles {
		if role == r {
			return true
		}
	}

	return false
}

// Permission is a capability a caller may or may not have.
//
// These are the questions handlers ask. They are deliberately coarse in Phase
// 0 — object-level, organization-scoped — because the fine-grained ones (which
// rows, which columns) belong to the query compiler, not to a handler. See
// architectural rule 1.
type Permission string

const (
	// PermManageOrganization covers renaming and settings.
	PermManageOrganization Permission = "manage_organization"

	// PermManageUsers covers creating, disabling and editing users.
	PermManageUsers Permission = "manage_users"

	// PermManageGroups covers groups and their membership.
	PermManageGroups Permission = "manage_groups"

	// PermManageRoles covers granting and revoking roles. Separate from
	// PermManageUsers because "can add a colleague" and "can make themselves an
	// administrator" are very different powers.
	PermManageRoles Permission = "manage_roles"

	// PermManageSessions covers ending another user's sessions. Part 6-b left
	// the organization-scoped revoke without a caller precisely because it
	// needed this gate first.
	PermManageSessions Permission = "manage_sessions"

	// PermManageConnections covers adding and editing data connections.
	PermManageConnections Permission = "manage_connections"

	// PermCreateContent covers authoring questions and dashboards.
	PermCreateContent Permission = "create_content"

	// PermViewContent is the floor: seeing what has been shared with you.
	PermViewContent Permission = "view_content"

	// PermQuery covers running a query through the semantic layer, where row
	// and column policies apply.
	PermQuery Permission = "query"

	// PermNativeQuery covers running arbitrary SQL, and is a separate grant
	// on purpose.
	//
	// Semantic-layer row-level security cannot constrain raw SQL: whoever can
	// write it can read anything the connection's credentials reach. Folding
	// this into an "editor can do editor things" bundle would quietly hand out
	// that power, so it is its own permission and the UI has to say what it
	// means. See security-model.md#native-sql-is-a-distinct-permission.
	PermNativeQuery Permission = "native_query"
)

// AllPermissions is every permission, for the role table's own test and for
// documentation generation.
var AllPermissions = []Permission{
	PermManageOrganization,
	PermManageUsers,
	PermManageGroups,
	PermManageRoles,
	PermManageSessions,
	PermManageConnections,
	PermCreateContent,
	PermViewContent,
	PermQuery,
	PermNativeQuery,
}

// rolePermissions maps each built-in role to what it may do.
//
// This table is the whole of Phase 0's policy, in one readable place, and the
// declarative harness in testdata asserts it end to end rather than restating
// it. Two choices in it are deliberate:
//
// Analyst has native_query and Editor does not. An editor builds and shares
// content; an analyst is trusted with the raw connection. Bundling raw SQL into
// "editor" would grant it to everyone who can make a dashboard.
//
// Viewer has query. Viewing a dashboard runs its queries, so a viewer who could
// not query could not view anything — but those queries go through the semantic
// layer, where policy applies.
var rolePermissions = map[Role]map[Permission]bool{
	RelationAdmin: {
		PermManageOrganization: true,
		PermManageUsers:        true,
		PermManageGroups:       true,
		PermManageRoles:        true,
		PermManageSessions:     true,
		PermManageConnections:  true,
		PermCreateContent:      true,
		PermViewContent:        true,
		PermQuery:              true,
		PermNativeQuery:        true,
	},
	RelationEditor: {
		PermCreateContent: true,
		PermViewContent:   true,
		PermQuery:         true,
	},
	RelationAnalyst: {
		PermCreateContent: true,
		PermViewContent:   true,
		PermQuery:         true,
		PermNativeQuery:   true,
	},
	RelationViewer: {
		PermViewContent: true,
		PermQuery:       true,
	},
}

// RoleGrants reports whether a role carries a permission.
func RoleGrants(role Role, perm Permission) bool {
	return rolePermissions[role][perm]
}

// PermissionsFor returns everything a role may do, for an API that shows a user
// what they can do rather than making them find out by being refused.
func PermissionsFor(role Role) []Permission {
	granted := rolePermissions[role]

	out := make([]Permission, 0, len(granted))

	for _, p := range AllPermissions {
		if granted[p] {
			out = append(out, p)
		}
	}

	return out
}

// RolesGranting returns every built-in role that carries a permission, most
// privileged first. The checker uses it to turn a permission question into the
// set of relations that would answer it yes.
func RolesGranting(perm Permission) []Role {
	out := make([]Role, 0, len(BuiltinRoles))

	for _, role := range BuiltinRoles {
		if RoleGrants(role, perm) {
			out = append(out, role)
		}
	}

	return out
}

// --- identifiers ----------------------------------------------------------

// Object is a resource a permission is checked against.
type Object struct {
	Type ObjectType
	ID   string
}

// String renders the Zanzibar form, "type:id".
func (o Object) String() string { return string(o.Type) + ":" + o.ID }

// Subject is who holds a relation.
//
// A Relation of "" is a direct subject: one user. A non-empty Relation makes it
// a userset — "group:analysts#member" is every member of that group — which is
// how a role is granted to a group.
type Subject struct {
	Type     SubjectType
	ID       string
	Relation Relation
}

// User builds a subject for one user.
func User(id string) Subject { return Subject{Type: SubjectUser, ID: id} }

// GroupMembers builds the userset of a group's members.
func GroupMembers(id string) Subject {
	return Subject{Type: SubjectGroup, ID: id, Relation: RelationMember}
}

// IsUserset reports whether the subject names a set of users rather than one.
func (s Subject) IsUserset() bool { return s.Relation != "" }

// String renders the Zanzibar form: "user:id" or "group:id#member".
func (s Subject) String() string {
	if s.IsUserset() {
		return fmt.Sprintf("%s:%s#%s", s.Type, s.ID, s.Relation)
	}

	return string(s.Type) + ":" + s.ID
}

// Tuple is one stored relationship: subject has relation on object.
type Tuple struct {
	Subject  Subject
	Relation Relation
	Object   Object
}

// String renders a tuple the way OpenFGA's own tooling does, which is what
// makes an assertion file readable and a log line greppable.
func (t Tuple) String() string {
	return fmt.Sprintf("%s#%s@%s", t.Object, t.Relation, t.Subject)
}

// ParseObject reads the "type:id" form.
func ParseObject(s string) (Object, error) {
	typ, id, found := strings.Cut(s, ":")
	if !found || typ == "" || id == "" {
		return Object{}, fmt.Errorf("authz: %q is not a valid object, want type:id", s)
	}

	return Object{Type: ObjectType(typ), ID: id}, nil
}

// ParseSubject reads the "type:id" and "type:id#relation" forms.
func ParseSubject(s string) (Subject, error) {
	base, relation, hasRelation := strings.Cut(s, "#")

	typ, id, found := strings.Cut(base, ":")
	if !found || typ == "" || id == "" {
		return Subject{}, fmt.Errorf("authz: %q is not a valid subject, want type:id or type:id#relation", s)
	}

	sub := Subject{Type: SubjectType(typ), ID: id}
	if hasRelation {
		if relation == "" {
			return Subject{}, fmt.Errorf("authz: %q has an empty relation after #", s)
		}

		sub.Relation = Relation(relation)
	}

	return sub, nil
}
