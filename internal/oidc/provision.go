package oidc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/logging"
	"github.com/Mmd4LIFE/pivot/internal/store/dbtypes"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

// AttributeSource is the provenance recorded on attributes from SSO.
//
// The user-attribute repository deletes by source, so a later sync replaces
// exactly what the identity provider owns and leaves anything set by hand
// alone. That separation is why provenance is stored rather than inferred.
const AttributeSource = "oidc"

// Errors provisioning may return.
var (
	// ErrNotProvisioned means the subject is unknown and the provider does not
	// create users. An organization that manages its user list out of band
	// wants exactly this.
	ErrNotProvisioned = errors.New("oidc: no account for this identity, and provisioning is off")

	// ErrNoEmail means the token carried no email and one is needed to create
	// a user.
	ErrNoEmail = errors.New("oidc: identity has no email claim")

	// ErrAccountDisabled means the matched user exists but is deactivated.
	ErrAccountDisabled = errors.New("oidc: the linked account is disabled")

	// ErrEmailNotVerified means account linking was attempted with an address
	// the provider has not vouched for.
	ErrEmailNotVerified = errors.New("oidc: cannot link an unverified email address")
)

// Provisioner turns a verified [Identity] into a Pivot user.
type Provisioner struct {
	repos *repo.Repositories
	log   *slog.Logger
}

// NewProvisioner builds a provisioner.
func NewProvisioner(repos *repo.Repositories, log *slog.Logger) *Provisioner {
	return &Provisioner{repos: repos, log: log}
}

// Result is a resolved login.
type Result struct {
	User model.User

	// Created reports whether this login brought the user into existence.
	Created bool

	// GroupsAdded and GroupsRemoved describe what the group sync changed, for
	// the audit log and for an administrator asking why someone's access moved.
	GroupsAdded   []string
	GroupsRemoved []string

	// UnknownGroups are provider groups with no matching Pivot group. Reported
	// rather than created: a directory with two hundred groups would otherwise
	// silently produce two hundred here on first login.
	UnknownGroups []string
}

// Provision resolves an identity to a user, creating one if configured to.
//
// The lookup order is the whole security argument. A returning user is found
// by (provider, subject) — the stable identifier — and by nothing else. After
// the first login the address is irrelevant to *which* account resolves:
// changing it in the directory moves the user's email, not their identity.
//
// Matching an existing local account by address happens only when the provider
// has LinkByEmail switched on, and then only for an address the provider says
// it has verified. Both conditions exist because of one attack: directories
// reassign addresses, and without them the next holder of ada@example.com
// would inherit the old Ada's account. The setting is for the window in which
// an organization migrates its existing users onto SSO, and is meant to be
// turned off afterwards.
func (p *Provisioner) Provision(
	ctx context.Context, orgID uuid.UUID, provider model.IdentityProvider, id Identity,
) (Result, error) {
	sys := p.repos.System()
	now := dbtypes.Now()

	scope, err := tenant.NewSystemScope(orgID)
	if err != nil {
		return Result{}, fmt.Errorf("oidc: %w", err)
	}

	scoped := tenant.WithScope(ctx, scope)

	// 1. A subject we have seen before.
	link, err := sys.FindFederatedIdentity(ctx, provider.ID, id.Subject)
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return Result{}, fmt.Errorf("oidc: look up federated identity: %w", err)
	}

	if err == nil {
		user, uerr := p.repos.Users.Get(scoped, link.UserID)
		if uerr != nil {
			return Result{}, fmt.Errorf("oidc: load linked user: %w", uerr)
		}

		if !user.IsActive.Bool() {
			return Result{}, ErrAccountDisabled
		}

		if rerr := sys.RecordFederatedLogin(ctx, provider.ID, id.Subject,
			dbtypes.NewNullTime(now.Time)); rerr != nil {
			p.log.Warn("could not record federated login", logging.Err(rerr))
		}

		// The directory is authoritative for the address, so a change there
		// follows through. This is safe precisely because identity was already
		// settled by the subject: updating the email cannot move the account.
		user, err = p.syncEmail(scoped, user, id)
		if err != nil {
			return Result{}, err
		}

		return p.sync(scoped, provider, id, user, false)
	}

	// 2. An existing local account with the same address, not yet linked.
	//
	// Off unless the provider opts in, and then only for a verified address.
	// See the doc comment above for why.
	email := repo.NormalizeEmail(id.Email)
	if email != "" && provider.LinkByEmail.Bool() {
		if !id.EmailVerified {
			p.log.Warn("refusing to link an unverified address",
				slog.String("provider", provider.Slug),
			)

			return Result{}, ErrEmailNotVerified
		}

		existing, eerr := p.repos.Users.GetByEmail(scoped, email)
		if eerr == nil {
			if !existing.IsActive.Bool() {
				return Result{}, ErrAccountDisabled
			}

			if _, lerr := sys.LinkFederatedIdentity(ctx, repo.LinkFederatedIdentity{
				OrgID:      orgID,
				ProviderID: provider.ID,
				UserID:     existing.ID,
				Subject:    id.Subject,
				At:         dbtypes.NewNullTime(now.Time),
			}); lerr != nil {
				return Result{}, fmt.Errorf("oidc: link existing account: %w", lerr)
			}

			p.log.Info("linked an existing account to an identity provider",
				slog.String("user_id", existing.ID.String()),
				slog.String("provider", provider.Slug),
			)

			return p.sync(scoped, provider, id, existing, false)
		}

		if !errors.Is(eerr, repo.ErrNotFound) {
			return Result{}, fmt.Errorf("oidc: look up user by email: %w", eerr)
		}
	}

	// 3. Nobody. Create, if this provider is allowed to.
	if !provider.AutoProvision.Bool() {
		return Result{}, ErrNotProvisioned
	}

	if email == "" {
		return Result{}, ErrNoEmail
	}

	name := id.Name
	if name == "" {
		name = email
	}

	// No password hash: an account created by SSO has no password, so it
	// cannot be logged into by the password path at all. internal/auth already
	// refuses a user whose hash is absent.
	user, err := p.repos.Users.Create(scoped, repo.CreateUser{
		Email:    email,
		Name:     name,
		IsActive: true,
	})
	if err != nil {
		return Result{}, fmt.Errorf("oidc: create user: %w", err)
	}

	if _, lerr := sys.LinkFederatedIdentity(ctx, repo.LinkFederatedIdentity{
		OrgID:      orgID,
		ProviderID: provider.ID,
		UserID:     user.ID,
		Subject:    id.Subject,
		At:         dbtypes.NewNullTime(now.Time),
	}); lerr != nil {
		return Result{}, fmt.Errorf("oidc: link new account: %w", lerr)
	}

	if provider.DefaultRole != "" && authz.IsBuiltinRole(authz.Relation(provider.DefaultRole)) {
		if gerr := p.repos.Roles.Grant(scoped, repo.GrantRole{
			SubjectType: "user",
			SubjectID:   user.ID,
			Relation:    provider.DefaultRole,
			ObjectType:  string(authz.TypeOrganization),
			ObjectID:    orgID,
		}); gerr != nil {
			return Result{}, fmt.Errorf("oidc: grant default role: %w", gerr)
		}
	}

	p.log.Info("provisioned a user from an identity provider",
		slog.String("user_id", user.ID.String()),
		slog.String("provider", provider.Slug),
		slog.String("role", provider.DefaultRole),
	)

	return p.sync(scoped, provider, id, user, true)
}

// syncEmail follows an address change made in the directory.
//
// Only ever called after identity has been settled by subject, so it moves a
// known user's address and can never move the account itself. A collision with
// another local account leaves the address alone and logs: refusing the login
// over it would lock someone out of a directory change they did not make.
func (p *Provisioner) syncEmail(
	ctx context.Context, user model.User, id Identity,
) (model.User, error) {
	email := repo.NormalizeEmail(id.Email)
	if email == "" || email == user.Email {
		return user, nil
	}

	updated, err := p.repos.Users.Update(ctx, repo.UpdateUser{
		ID:       user.ID,
		Email:    email,
		Name:     nonEmpty(id.Name, user.Name),
		IsActive: user.IsActive.Bool(),
		Locale:   user.Locale,
		Timezone: user.Timezone,
		Version:  user.Version,
	})
	if err != nil {
		if errors.Is(err, repo.ErrDuplicate) {
			p.log.Warn("the directory's address is already used by another account",
				slog.String("user_id", user.ID.String()),
				slog.String("email", email),
			)

			return user, nil
		}

		return user, fmt.Errorf("oidc: update email: %w", err)
	}

	return updated, nil
}

// nonEmpty returns the first non-empty value.
func nonEmpty(preferred, fallback string) string {
	if preferred != "" {
		return preferred
	}

	return fallback
}

// sync applies the identity's claims to an existing user.
func (p *Provisioner) sync(
	ctx context.Context, provider model.IdentityProvider,
	id Identity, user model.User, created bool,
) (Result, error) {
	result := Result{User: user, Created: created}

	if err := p.syncAttributes(ctx, user.ID, id); err != nil {
		return result, err
	}

	added, removed, unknown, err := p.syncGroups(ctx, user.ID, id)
	if err != nil {
		return result, err
	}

	result.GroupsAdded = added
	result.GroupsRemoved = removed
	result.UnknownGroups = unknown

	if len(unknown) > 0 {
		// Logged once per login rather than silently dropped: an administrator
		// debugging "why is this person not in the analysts group" needs to see
		// that the group does not exist on this side.
		p.log.Info("identity provider named groups that do not exist here",
			slog.String("user_id", user.ID.String()),
			slog.String("provider", provider.Slug),
			slog.Any("groups", unknown),
		)
	}

	return result, nil
}

// syncAttributes replaces the attributes this provider owns.
//
// Delete-by-source then re-upsert, rather than upserting the new set and
// leaving the rest: a claim that disappears from the token must disappear from
// the user, or a revoked attribute lingers forever and Phase 4's row-level
// security keeps honoring it.
func (p *Provisioner) syncAttributes(ctx context.Context, userID uuid.UUID, id Identity) error {
	if _, err := p.repos.UserAttributes.DeleteBySource(ctx, userID, AttributeSource); err != nil {
		return fmt.Errorf("oidc: clear previous attributes: %w", err)
	}

	for key, value := range id.Attributes {
		if _, err := p.repos.UserAttributes.Set(ctx, repo.SetAttribute{
			UserID: userID,
			Key:    key,
			Value:  value,
			Source: AttributeSource,
		}); err != nil {
			return fmt.Errorf("oidc: store attribute %q: %w", key, err)
		}
	}

	return nil
}

// syncGroups reconciles Pivot group membership with the provider's claim.
//
// Groups are matched by name and are **never created**. A directory routinely
// carries hundreds of groups that mean nothing here, and auto-creating them
// would fill the organization with empty groups that an administrator then has
// to identify and delete. Unmatched names are reported instead.
//
// Only memberships this sync could have created are removed — that is, the
// user is removed from a group only if it is absent from the claim. A group
// added by hand and not named by the provider is therefore removed too, which
// is the correct behavior for a directory-driven deployment and is worth
// knowing before turning SSO on. Phase 4 adds provenance to membership so the
// two can coexist.
func (p *Provisioner) syncGroups(
	ctx context.Context, userID uuid.UUID, id Identity,
) (added, removed, unknown []string, err error) {
	claimed := make(map[string]bool, len(id.Groups))
	for _, name := range id.Groups {
		claimed[strings.TrimSpace(name)] = true
	}

	current, err := p.repos.Groups.ListForUser(ctx, userID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("oidc: list current groups: %w", err)
	}

	have := make(map[string]model.Group, len(current))
	for _, g := range current {
		have[g.Name] = g
	}

	for name := range claimed {
		if name == "" || have[name].ID != uuid.Nil {
			continue
		}

		group, gerr := p.repos.Groups.GetByName(ctx, name)
		if gerr != nil {
			if errors.Is(gerr, repo.ErrNotFound) {
				unknown = append(unknown, name)

				continue
			}

			return nil, nil, nil, fmt.Errorf("oidc: look up group %q: %w", name, gerr)
		}

		if aerr := p.repos.Groups.AddMember(ctx, group.ID, userID); aerr != nil {
			return nil, nil, nil, fmt.Errorf("oidc: add to group %q: %w", name, aerr)
		}

		added = append(added, name)
	}

	for name, group := range have {
		if claimed[name] {
			continue
		}

		if rerr := p.repos.Groups.RemoveMember(ctx, group.ID, userID); rerr != nil {
			return nil, nil, nil, fmt.Errorf("oidc: remove from group %q: %w", name, rerr)
		}

		removed = append(removed, name)
	}

	sortStrings(added)
	sortStrings(removed)
	sortStrings(unknown)

	return added, removed, unknown, nil
}

// sortStrings sorts in place, so results are stable for assertions and logs.
func sortStrings(in []string) {
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j] < in[j-1]; j-- {
			in[j], in[j-1] = in[j-1], in[j]
		}
	}
}
