package oidc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/oidc"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

// storedProvider creates an identity provider row with the safe defaults:
// provisioning as asked, and email linking off.
func storedProvider(t *testing.T, f *fixture, autoProvision bool) model.IdentityProvider {
	t.Helper()

	return storedProviderWith(t, f, repo.CreateIdentityProvider{
		Slug:          "test",
		Name:          "Test IdP",
		Issuer:        "https://idp.example",
		ClientID:      "pivot",
		IsEnabled:     true,
		AutoProvision: autoProvision,
		DefaultRole:   string(authz.RelationViewer),
	})
}

// storedProviderWith creates a provider from an explicit configuration.
func storedProviderWith(
	t *testing.T, f *fixture, in repo.CreateIdentityProvider,
) model.IdentityProvider {
	t.Helper()

	provider, err := f.repos.IdentityProviders.Create(f.ctx, in)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	return provider
}

// linkingProvider creates a provider with email linking switched on, which is
// what an organization migrating its existing users onto SSO would configure.
func linkingProvider(t *testing.T, f *fixture) model.IdentityProvider {
	t.Helper()

	return storedProviderWith(t, f, repo.CreateIdentityProvider{
		Slug:          "linking",
		Name:          "Linking IdP",
		Issuer:        "https://idp.example",
		ClientID:      "pivot",
		IsEnabled:     true,
		AutoProvision: true,
		LinkByEmail:   true,
		DefaultRole:   string(authz.RelationViewer),
	})
}

// A first login creates the user, links the subject, grants the default role,
// and records the claims.
func TestProvisionCreatesAUser(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newFixture(t, db)
		provider := storedProvider(t, f, true)

		identity := oidc.Identity{
			Subject: "subject-ada",
			Email:   "Ada@Example.com",
			Name:    "Ada Lovelace",
			Groups:  []string{"analysts"},
			Attributes: map[string]string{
				"department": "research",
			},
		}

		// The group has to exist: provisioning never creates them.
		if _, err := f.repos.Groups.Create(f.ctx, repo.CreateGroup{Name: "analysts"}); err != nil {
			t.Fatalf("create group: %v", err)
		}

		result, err := f.provisioner.Provision(context.Background(), f.org.ID, provider, identity)
		if err != nil {
			t.Fatalf("provision: %v", err)
		}

		if !result.Created {
			t.Error("the first login did not report creating the user")
		}

		// The address is normalized on the way in, so a directory that sends
		// mixed case does not produce a second account later.
		if result.User.Email != "ada@example.com" {
			t.Errorf("email = %q, want it normalized", result.User.Email)
		}

		// An account created by SSO has no password, so the password login
		// path cannot be used against it at all.
		if result.User.PasswordHash.Valid && result.User.PasswordHash.String != "" {
			t.Error("a provisioned user was given a password hash")
		}

		if len(result.GroupsAdded) != 1 || result.GroupsAdded[0] != "analysts" {
			t.Errorf("groups added = %v, want [analysts]", result.GroupsAdded)
		}

		attrs, err := f.repos.UserAttributes.Map(f.ctx, result.User.ID)
		if err != nil {
			t.Fatalf("read attributes: %v", err)
		}

		if attrs["department"] != "research" {
			t.Errorf("attributes = %v, want department=research", attrs)
		}

		// The default role must actually be granted, or the new user can log
		// in and do nothing.
		checker, _ := authz.New(f.repos)

		decision, err := checker.Check(f.ctx, authz.Request{
			Subject:    authz.User(result.User.ID.String()),
			Permission: authz.PermViewContent,
			Object:     authz.Object{Type: authz.TypeOrganization, ID: f.org.ID.String()},
		})
		if err != nil {
			t.Fatalf("check: %v", err)
		}

		if !decision.Allowed {
			t.Error("the provisioned user did not receive the default role")
		}
	})
}

// The claims must land with source 'oidc', so a later sync can replace exactly
// what the provider owns and leave anything set by hand alone.
func TestProvisionedAttributesCarryTheirProvenance(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newFixture(t, db)
		provider := storedProvider(t, f, true)

		result, err := f.provisioner.Provision(context.Background(), f.org.ID, provider,
			oidc.Identity{
				Subject:    "subject-ada",
				Email:      "ada@example.com",
				Attributes: map[string]string{"department": "research"},
			})
		if err != nil {
			t.Fatalf("provision: %v", err)
		}

		attrs, err := f.repos.UserAttributes.List(f.ctx, result.User.ID)
		if err != nil {
			t.Fatalf("list attributes: %v", err)
		}

		if len(attrs) != 1 {
			t.Fatalf("got %d attributes, want 1", len(attrs))
		}

		if attrs[0].Source != oidc.AttributeSource {
			t.Errorf("source = %q, want %q", attrs[0].Source, oidc.AttributeSource)
		}
	})
}

// A returning user is matched on the subject, never the email.
//
// This is the security property that matters most in this file. If matching
// were by address, then an address reassigned inside a directory would hand
// the new holder the old user's account and everything it can reach.
func TestReturningUserIsMatchedOnSubjectNotEmail(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newFixture(t, db)
		provider := storedProvider(t, f, true)
		ctx := context.Background()

		first, err := f.provisioner.Provision(ctx, f.org.ID, provider, oidc.Identity{
			Subject: "subject-ada", Email: "ada@example.com", Name: "Ada",
		})
		if err != nil {
			t.Fatalf("first login: %v", err)
		}

		// Same person, new address — a marriage, a rebrand, an IT policy.
		second, err := f.provisioner.Provision(ctx, f.org.ID, provider, oidc.Identity{
			Subject: "subject-ada", Email: "ada.lovelace@example.com", Name: "Ada",
		})
		if err != nil {
			t.Fatalf("second login: %v", err)
		}

		if second.User.ID != first.User.ID {
			t.Error("a changed email produced a different user; matching is not on subject")
		}

		if second.Created {
			t.Error("a returning user was reported as created")
		}

		// The directory is authoritative for the address, so the change
		// follows through - safely, because identity was settled by subject
		// before the email was touched.
		if second.User.Email != "ada.lovelace@example.com" {
			t.Errorf("email = %q, want the directory's new address", second.User.Email)
		}

		// A different person who has inherited the old address must NOT get
		// the old account.
		third, err := f.provisioner.Provision(ctx, f.org.ID, provider, oidc.Identity{
			Subject: "subject-someone-else", Email: "ada@example.com",
			EmailVerified: true, Name: "Somebody Else",
		})
		if err != nil {
			t.Fatalf("third login: %v", err)
		}

		if third.User.ID == first.User.ID {
			t.Error("a different subject with a recycled address was given the original account")
		}
	})
}

// Linking an existing account by address is OFF unless the provider opts in.
//
// This is the default that matters. With linking on by default, the next
// holder of a recycled address inherits the previous holder's account — and
// nobody configuring SSO would have chosen that, because nobody is asked.
func TestEmailLinkingIsOffByDefault(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newFixture(t, db)
		provider := storedProvider(t, f, true)

		existing, err := f.repos.Users.Create(f.ctx, repo.CreateUser{
			Email: "ada@example.com", Name: "Ada", IsActive: true,
		})
		if err != nil {
			t.Fatalf("create user: %v", err)
		}

		// A verified address matching an existing account, and still no link:
		// the provider did not ask for that behavior.
		_, err = f.provisioner.Provision(context.Background(), f.org.ID, provider,
			oidc.Identity{
				Subject: "a-stranger", Email: "ada@example.com", EmailVerified: true,
			})

		// Creating a second account fails on the unique email index, which is
		// the correct outcome: the login is refused rather than adopting
		// somebody else's account.
		if err == nil {
			t.Fatal("an unlinked subject silently took over a matching account")
		}

		links, lerr := f.repos.IdentityProviders.LinkedIdentities(f.ctx, existing.ID)
		if lerr != nil {
			t.Fatalf("list links: %v", lerr)
		}

		if len(links) != 0 {
			t.Errorf("the existing account was linked anyway: %v", links)
		}
	})
}

// Even with linking on, an unverified address must not adopt an account.
//
// email_verified is the provider's assertion that it controls the address. An
// identity provider that lets a user type any address without proving it -
// and several do - would otherwise be a way to claim any account by name.
func TestUnverifiedEmailIsNotLinked(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newFixture(t, db)
		provider := linkingProvider(t, f)

		if _, err := f.repos.Users.Create(f.ctx, repo.CreateUser{
			Email: "ada@example.com", Name: "Ada", IsActive: true,
		}); err != nil {
			t.Fatalf("create user: %v", err)
		}

		_, err := f.provisioner.Provision(context.Background(), f.org.ID, provider,
			oidc.Identity{
				Subject: "a-stranger", Email: "ada@example.com", EmailVerified: false,
			})

		if !errors.Is(err, oidc.ErrEmailNotVerified) {
			t.Errorf("error = %v, want ErrEmailNotVerified", err)
		}
	})
}

// An existing local account is linked rather than duplicated, so turning SSO
// on does not orphan everyone's existing account.
func TestExistingAccountIsLinkedOnFirstSSOLogin(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newFixture(t, db)
		provider := linkingProvider(t, f)

		existing, err := f.repos.Users.Create(f.ctx, repo.CreateUser{
			Email: "ada@example.com", Name: "Ada", IsActive: true,
		})
		if err != nil {
			t.Fatalf("create user: %v", err)
		}

		result, err := f.provisioner.Provision(context.Background(), f.org.ID, provider,
			oidc.Identity{
				Subject: "subject-ada", Email: "ada@example.com", EmailVerified: true,
			})
		if err != nil {
			t.Fatalf("provision: %v", err)
		}

		if result.User.ID != existing.ID {
			t.Error("SSO created a second account instead of linking the existing one")
		}

		if result.Created {
			t.Error("linking an existing account was reported as creating one")
		}

		links, err := f.repos.IdentityProviders.LinkedIdentities(f.ctx, existing.ID)
		if err != nil {
			t.Fatalf("list links: %v", err)
		}

		if len(links) != 1 || links[0].Subject != "subject-ada" {
			t.Errorf("links = %v, want one for subject-ada", links)
		}
	})
}

// With provisioning off, an unknown subject is refused rather than created.
func TestUnknownSubjectIsRefusedWhenProvisioningIsOff(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newFixture(t, db)
		provider := storedProvider(t, f, false)

		_, err := f.provisioner.Provision(context.Background(), f.org.ID, provider,
			oidc.Identity{Subject: "stranger", Email: "stranger@example.com"})

		if !errors.Is(err, oidc.ErrNotProvisioned) {
			t.Errorf("error = %v, want ErrNotProvisioned", err)
		}

		count, cerr := f.repos.Users.Count(f.ctx)
		if cerr != nil {
			t.Fatalf("count: %v", cerr)
		}

		if count != 0 {
			t.Errorf("%d users exist; provisioning was supposed to be off", count)
		}
	})
}

// A disabled account cannot be logged into over SSO.
//
// Deactivating a user must actually stop them getting in, by every route. An
// SSO path that ignored is_active would be a way around the off switch.
func TestDisabledAccountIsRefused(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newFixture(t, db)
		provider := storedProvider(t, f, true)
		ctx := context.Background()

		result, err := f.provisioner.Provision(ctx, f.org.ID, provider,
			oidc.Identity{Subject: "subject-ada", Email: "ada@example.com"})
		if err != nil {
			t.Fatalf("provision: %v", err)
		}

		current, err := f.repos.Users.Get(f.ctx, result.User.ID)
		if err != nil {
			t.Fatalf("get user: %v", err)
		}

		if _, uerr := f.repos.Users.Update(f.ctx, repo.UpdateUser{
			ID:       current.ID,
			Email:    current.Email,
			Name:     current.Name,
			IsActive: false,
			Locale:   current.Locale,
			Timezone: current.Timezone,
			Version:  current.Version,
		}); uerr != nil {
			t.Fatalf("disable: %v", uerr)
		}

		_, err = f.provisioner.Provision(ctx, f.org.ID, provider,
			oidc.Identity{Subject: "subject-ada", Email: "ada@example.com"})

		if !errors.Is(err, oidc.ErrAccountDisabled) {
			t.Errorf("error = %v, want ErrAccountDisabled", err)
		}
	})
}

// Group membership follows the directory in both directions, and a claim
// naming a group that does not exist is reported rather than creating one.
func TestGroupSyncFollowsTheDirectory(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newFixture(t, db)
		provider := storedProvider(t, f, true)
		ctx := context.Background()

		for _, name := range []string{"analysts", "editors"} {
			if _, err := f.repos.Groups.Create(f.ctx, repo.CreateGroup{Name: name}); err != nil {
				t.Fatalf("create group %s: %v", name, err)
			}
		}

		first, err := f.provisioner.Provision(ctx, f.org.ID, provider, oidc.Identity{
			Subject: "subject-ada", Email: "ada@example.com",
			Groups: []string{"analysts", "editors"},
		})
		if err != nil {
			t.Fatalf("first: %v", err)
		}

		if len(first.GroupsAdded) != 2 {
			t.Errorf("added = %v, want both groups", first.GroupsAdded)
		}

		// The directory drops one and names one that does not exist here.
		second, err := f.provisioner.Provision(ctx, f.org.ID, provider, oidc.Identity{
			Subject: "subject-ada", Email: "ada@example.com",
			Groups: []string{"analysts", "no-such-group"},
		})
		if err != nil {
			t.Fatalf("second: %v", err)
		}

		if len(second.GroupsRemoved) != 1 || second.GroupsRemoved[0] != "editors" {
			t.Errorf("removed = %v, want [editors]", second.GroupsRemoved)
		}

		if len(second.UnknownGroups) != 1 || second.UnknownGroups[0] != "no-such-group" {
			t.Errorf("unknown = %v, want [no-such-group]", second.UnknownGroups)
		}

		// Reported, never created: a directory with hundreds of groups would
		// otherwise fill the organization with empty ones.
		groups, err := f.repos.Groups.List(f.ctx, 100, 0)
		if err != nil {
			t.Fatalf("list groups: %v", err)
		}

		if len(groups) != 2 {
			t.Errorf("%d groups exist, want 2; the sync created one", len(groups))
		}
	})
}

// An attribute that disappears from the token must disappear from the user, or
// a revoked one lingers and Phase 4's row-level security keeps honoring it.
func TestRemovedClaimsAreRemovedFromTheUser(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newFixture(t, db)
		provider := storedProvider(t, f, true)
		ctx := context.Background()

		result, err := f.provisioner.Provision(ctx, f.org.ID, provider, oidc.Identity{
			Subject: "subject-ada", Email: "ada@example.com",
			Attributes: map[string]string{"department": "research", "clearance": "high"},
		})
		if err != nil {
			t.Fatalf("first: %v", err)
		}

		// An attribute set by hand must survive the sync: it is not the
		// provider's to remove.
		if _, serr := f.repos.UserAttributes.Set(f.ctx, repo.SetAttribute{
			UserID: result.User.ID, Key: "nickname", Value: "Countess", Source: "manual",
		}); serr != nil {
			t.Fatalf("set manual attribute: %v", serr)
		}

		if _, serr := f.provisioner.Provision(ctx, f.org.ID, provider, oidc.Identity{
			Subject: "subject-ada", Email: "ada@example.com",
			Attributes: map[string]string{"department": "research"},
		}); serr != nil {
			t.Fatalf("second: %v", serr)
		}

		attrs, err := f.repos.UserAttributes.Map(f.ctx, result.User.ID)
		if err != nil {
			t.Fatalf("read attributes: %v", err)
		}

		if _, present := attrs["clearance"]; present {
			t.Error("a claim removed from the token survived the sync")
		}

		if attrs["department"] != "research" {
			t.Errorf("department = %q, want research", attrs["department"])
		}

		if attrs["nickname"] != "Countess" {
			t.Error("the sync removed an attribute set by hand; it owns only its own source")
		}
	})
}

// A provider belongs to one organization, and a login cannot reach across.
func TestProvisioningIsScopedToTheOrganization(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newFixture(t, db)
		provider := storedProvider(t, f, true)

		other, err := f.repos.System().CreateOrganization(context.Background(),
			repo.CreateOrganization{Name: "Globex", Slug: "globex"})
		if err != nil {
			t.Fatalf("create org: %v", err)
		}

		// The same provider row, used against a different organization: the
		// composite foreign key on (provider_id, org_id) is what refuses it.
		_, err = f.provisioner.Provision(context.Background(), other.ID, provider,
			oidc.Identity{Subject: "subject-ada", Email: "ada@example.com"})

		if err == nil {
			t.Fatal("a provider was used to provision into another organization")
		}
	})
}

// The unscoped lookups login needs must still be tenant-correct.
func TestFindIdentityProviderIsTenantCorrect(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newFixture(t, db)
		storedProvider(t, f, true)

		other, err := f.repos.System().CreateOrganization(context.Background(),
			repo.CreateOrganization{Name: "Globex", Slug: "globex"})
		if err != nil {
			t.Fatalf("create org: %v", err)
		}

		sys := f.repos.System()
		ctx := context.Background()

		if _, err := sys.FindIdentityProvider(ctx, f.org.ID, "test"); err != nil {
			t.Errorf("provider not found in its own organization: %v", err)
		}

		if _, err := sys.FindIdentityProvider(ctx, other.ID, "test"); !errors.Is(err, repo.ErrNotFound) {
			t.Errorf("error = %v, want ErrNotFound for another tenant's slug", err)
		}
	})
}

var _ = uuid.Nil
