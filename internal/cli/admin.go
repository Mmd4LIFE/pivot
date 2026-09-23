package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Mmd4LIFE/pivot/internal/auth"
	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/config"
	"github.com/Mmd4LIFE/pivot/internal/logging"
	"github.com/Mmd4LIFE/pivot/internal/setup"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

func newAdminCmd(env Env, flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Administrative commands",
		Long: `Administrative commands that operate directly on the database.

These run without authentication, because they exist for the cases where
nobody can log in yet: a fresh install with no users, or a locked-out
administrator. Anyone who can run them already has the database credentials.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newCreateUserCmd(env, flags),
		newResetPasswordCmd(env, flags),
		newGrantRoleCmd(env, flags),
		newRevokeRoleCmd(env, flags),
		newAddProviderCmd(env, flags),
	)

	return cmd
}

// readPassword prompts twice without echoing.
//
// Reading from a terminal rather than a flag is deliberate: a password passed
// as an argument lands in the shell history and in the process list, where any
// other user on the machine can read it.
func readPassword(env Env, prompt string) (string, error) {
	fd := int(os.Stdin.Fd())

	if !term.IsTerminal(fd) {
		return "", errors.New(
			"not a terminal: run this interactively, or set PIVOT_ADMIN_PASSWORD")
	}

	fmt.Fprint(env.Stderr, prompt)

	first, err := term.ReadPassword(fd)
	fmt.Fprintln(env.Stderr)

	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}

	fmt.Fprint(env.Stderr, "Confirm password: ")

	second, err := term.ReadPassword(fd)
	fmt.Fprintln(env.Stderr)

	if err != nil {
		return "", fmt.Errorf("read confirmation: %w", err)
	}

	if !bytes.Equal(first, second) {
		return "", errors.New("the passwords do not match")
	}

	return string(first), nil
}

// resolvePassword takes the password from the environment or prompts for it.
//
// The environment variable exists for automated provisioning — a container
// entrypoint creating the first administrator — and is documented as the less
// safe option, because an environment variable is readable by anything that
// can inspect the process.
func resolvePassword(env Env, prompt string) (string, error) {
	lookup := env.Lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}

	if pw, ok := lookup("PIVOT_ADMIN_PASSWORD"); ok && pw != "" {
		return pw, nil
	}

	return readPassword(env, prompt)
}

// openRepos connects and returns the repository set.
func openRepos(cmd *cobra.Command, env Env, flags *globalFlags) (*store.DB, *repo.Repositories, error) {
	res, err := loadConfig(cmd, env, flags)
	if err != nil {
		return nil, nil, err
	}

	log := logging.New(res.Config.Log, env.Stderr)

	db, err := store.Open(cmd.Context(), res.Config.Database, log)
	if err != nil {
		return nil, nil, err
	}

	return db, repo.New(db), nil
}

// resolveOrg finds the organization to act on.
//
// With no slug and exactly one organization, that one is used — which is the
// single-tenant self-hosted case and keeps the first-run experience to one
// command. With several, the slug is required, because guessing which tenant
// an administrator meant is the wrong kind of helpful.
func resolveOrg(cmd *cobra.Command, repos *repo.Repositories, slug string) (uuid.UUID, string, error) {
	sys := repos.System()

	if slug != "" {
		org, err := sys.GetOrganizationBySlug(cmd.Context(), slug)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				return uuid.Nil, "", fmt.Errorf("no organization with slug %q", slug)
			}

			return uuid.Nil, "", err
		}

		return org.ID, org.Slug, nil
	}

	orgs, err := sys.ListOrganizations(cmd.Context(), 2, 0)
	if err != nil {
		return uuid.Nil, "", err
	}

	switch len(orgs) {
	case 0:
		return uuid.Nil, "", errors.New(
			"no organizations exist yet; create one with --create-org")
	case 1:
		return orgs[0].ID, orgs[0].Slug, nil
	default:
		return uuid.Nil, "", errors.New(
			"more than one organization exists; name one with --org")
	}
}

func newCreateUserCmd(env Env, flags *globalFlags) *cobra.Command {
	var (
		email     string
		name      string
		orgSlug   string
		createOrg string
	)

	cmd := &cobra.Command{
		Use:   "create-user",
		Short: "Create a user with a password",
		Long: `Create a user and set their password.

The password is read from the terminal without echoing. For automated
provisioning set PIVOT_ADMIN_PASSWORD instead — less safe, because an
environment variable is readable by anything that can inspect the process.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if email == "" {
				return errors.New("--email is required")
			}

			db, repos, err := openRepos(cmd, env, flags)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			var (
				orgID uuid.UUID
				slug  string
			)

			if createOrg != "" {
				org, cerr := repos.System().CreateOrganization(cmd.Context(), repo.CreateOrganization{
					Name: createOrg,
					Slug: slugify(createOrg),
				})
				if cerr != nil {
					return fmt.Errorf("create organization: %w", cerr)
				}

				orgID, slug = org.ID, org.Slug
				fmt.Fprintf(env.Stdout, "Created organization %q (%s).\n", org.Name, org.Slug)
			} else {
				orgID, slug, err = resolveOrg(cmd, repos, orgSlug)
				if err != nil {
					return err
				}
			}

			password, err := resolvePassword(env, "Password: ")
			if err != nil {
				return err
			}

			hash, err := auth.HashPassword(password)
			if err != nil {
				return err
			}

			// Administrative commands have no session, so the scope is built
			// from the resolved organization with no actor. created_by records
			// that honestly rather than inventing a user.
			scope, err := tenant.NewSystemScope(orgID)
			if err != nil {
				return err
			}

			ctx := tenant.WithScope(cmd.Context(), scope)

			// Shared with the browser's setup wizard, deliberately. "The first
			// user becomes the administrator" is a rule about privilege, and a
			// rule about privilege that exists in two implementations is a rule
			// that is eventually only true in one of them.
			user, granted, err := setup.CreateUser(ctx, repos, orgID, setup.CreateUserRequest{
				Email:        email,
				Name:         name,
				PasswordHash: hash,
			})
			if err != nil {
				if errors.Is(err, repo.ErrDuplicate) {
					return fmt.Errorf("a user with email %q already exists in %q", email, slug)
				}

				return err
			}

			fmt.Fprintf(env.Stdout, "Created user %s (%s) in organization %s.\n",
				user.Email, user.ID, slug)

			if granted {
				fmt.Fprintf(env.Stdout,
					"Granted the admin role: %s is the first user in %s.\n", user.Email, slug)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "Email address (required)")
	cmd.Flags().StringVar(&name, "name", "", "Display name")
	cmd.Flags().StringVar(&orgSlug, "org", "", "Organization slug; omit when only one exists")
	cmd.Flags().StringVar(&createOrg, "create-org", "", "Create an organization with this name first")

	return cmd
}

func newResetPasswordCmd(env Env, flags *globalFlags) *cobra.Command {
	var (
		email   string
		orgSlug string
	)

	cmd := &cobra.Command{
		Use:   "reset-password",
		Short: "Set a user's password and end their sessions",
		Long: `Set a user's password.

Every existing session for that user is revoked. A password reset is usually a
response to suspected compromise, and leaving the attacker's session alive
would defeat it.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if email == "" {
				return errors.New("--email is required")
			}

			db, repos, err := openRepos(cmd, env, flags)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			orgID, slug, err := resolveOrg(cmd, repos, orgSlug)
			if err != nil {
				return err
			}

			scope, err := tenant.NewSystemScope(orgID)
			if err != nil {
				return err
			}

			ctx := tenant.WithScope(cmd.Context(), scope)

			user, err := repos.Users.GetByEmail(ctx, repo.NormalizeEmail(email))
			if err != nil {
				if errors.Is(err, repo.ErrNotFound) {
					return fmt.Errorf("no user with email %q in %q", email, slug)
				}

				return err
			}

			password, err := resolvePassword(env, "New password: ")
			if err != nil {
				return err
			}

			log := logging.New(loadedLogConfig(cmd, env, flags), env.Stderr)
			svc := auth.NewService(repos, auth.DefaultPolicy(), log)

			if serr := svc.SetPassword(ctx, user.ID, password); serr != nil {
				return serr
			}

			// Clear any lockout too: an administrator resetting a password is
			// almost always unblocking someone who is locked out.
			if cerr := repos.System().ClearLoginAttempts(cmd.Context(), orgID, user.Email); cerr != nil {
				fmt.Fprintf(env.Stderr, "warning: could not clear lockout: %v\n", cerr)
			}

			fmt.Fprintf(env.Stdout,
				"Password set for %s. All existing sessions revoked.\n", user.Email)

			return nil
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "Email address (required)")
	cmd.Flags().StringVar(&orgSlug, "org", "", "Organization slug; omit when only one exists")

	return cmd
}

// loadedLogConfig re-reads log settings for a command that needs a logger.
func loadedLogConfig(cmd *cobra.Command, env Env, flags *globalFlags) config.LogConfig {
	res, err := loadConfig(cmd, env, flags)
	if err != nil {
		return config.Default().Log
	}

	return res.Config.Log
}

// slugify turns a name into a URL-safe slug.
// slugify is [setup.Slugify], kept as a local name because this file reads
// better for it. One implementation, shared with the setup wizard, so a CLI
// organization and a browser organization cannot end up with different slugs
// for the same name.
func slugify(in string) string { return setup.Slugify(in) }

// resolveRoleTarget finds the user a role command names.
func resolveRoleTarget(
	cmd *cobra.Command, repos *repo.Repositories, orgID uuid.UUID, slug, email string,
) (uuid.UUID, error) {
	scope, err := tenant.NewSystemScope(orgID)
	if err != nil {
		return uuid.Nil, err
	}

	ctx := tenant.WithScope(cmd.Context(), scope)

	user, err := repos.Users.GetByEmail(ctx, repo.NormalizeEmail(email))
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return uuid.Nil, fmt.Errorf("no user with email %q in %q", email, slug)
		}

		return uuid.Nil, err
	}

	return user.ID, nil
}

// validRole checks a role name against the built-in set.
func validRole(role string) error {
	if authz.IsBuiltinRole(authz.Relation(role)) {
		return nil
	}

	names := make([]string, 0, len(authz.BuiltinRoles))
	for _, r := range authz.BuiltinRoles {
		names = append(names, string(r))
	}

	return fmt.Errorf("unknown role %q; expected one of %s", role, strings.Join(names, ", "))
}

func newGrantRoleCmd(env Env, flags *globalFlags) *cobra.Command {
	var email, role, orgSlug string

	cmd := &cobra.Command{
		Use:   "grant-role",
		Short: "Give a user a role",
		Long: `Give a user one of the built-in roles.

This is the recovery path for an organization whose administrators have all
been removed or locked out. It bypasses the permission check that the HTTP
endpoint applies, which is safe for the same reason the other admin commands
are: anyone who can run it already has the database credentials.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if email == "" {
				return errors.New("--email is required")
			}

			if err := validRole(role); err != nil {
				return err
			}

			db, repos, err := openRepos(cmd, env, flags)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			orgID, slug, err := resolveOrg(cmd, repos, orgSlug)
			if err != nil {
				return err
			}

			userID, err := resolveRoleTarget(cmd, repos, orgID, slug, email)
			if err != nil {
				return err
			}

			scope, err := tenant.NewSystemScope(orgID)
			if err != nil {
				return err
			}

			ctx := tenant.WithScope(cmd.Context(), scope)

			if gerr := repos.Roles.Grant(ctx, repo.GrantRole{
				SubjectType: "user",
				SubjectID:   userID,
				Relation:    role,
				ObjectType:  string(authz.TypeOrganization),
				ObjectID:    orgID,
			}); gerr != nil {
				return gerr
			}

			fmt.Fprintf(env.Stdout, "Granted %s to %s in %s.\n", role, email, slug)

			return nil
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "Email address (required)")
	cmd.Flags().StringVar(&role, "role", "admin", "Role to grant")
	cmd.Flags().StringVar(&orgSlug, "org", "", "Organization slug; omit when only one exists")

	return cmd
}

func newRevokeRoleCmd(env Env, flags *globalFlags) *cobra.Command {
	var email, role, orgSlug string

	cmd := &cobra.Command{
		Use:   "revoke-role",
		Short: "Take a role away from a user",
		Long: `Remove one of the built-in roles from a user.

Unlike the HTTP endpoint, this does not refuse to remove the last
administrator: an operator with database access is expected to know what they
are doing, and a recovery tool that argues with you is not much of one.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if email == "" {
				return errors.New("--email is required")
			}

			if err := validRole(role); err != nil {
				return err
			}

			db, repos, err := openRepos(cmd, env, flags)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			orgID, slug, err := resolveOrg(cmd, repos, orgSlug)
			if err != nil {
				return err
			}

			userID, err := resolveRoleTarget(cmd, repos, orgID, slug, email)
			if err != nil {
				return err
			}

			scope, err := tenant.NewSystemScope(orgID)
			if err != nil {
				return err
			}

			ctx := tenant.WithScope(cmd.Context(), scope)

			if rerr := repos.Roles.Revoke(ctx, repo.GrantRole{
				SubjectType: "user",
				SubjectID:   userID,
				Relation:    role,
				ObjectType:  string(authz.TypeOrganization),
				ObjectID:    orgID,
			}); rerr != nil {
				return rerr
			}

			// Warn rather than refuse, and say so plainly.
			holders, cerr := repos.Roles.CountHolders(ctx,
				string(authz.RelationAdmin), string(authz.TypeOrganization), orgID)
			if cerr == nil && holders == 0 {
				fmt.Fprintf(env.Stderr,
					"warning: %s now has no administrators; "+
						"use `pivot admin grant-role` to appoint one\n", slug)
			}

			fmt.Fprintf(env.Stdout, "Revoked %s from %s in %s.\n", role, email, slug)

			return nil
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "Email address (required)")
	cmd.Flags().StringVar(&role, "role", "admin", "Role to revoke")
	cmd.Flags().StringVar(&orgSlug, "org", "", "Organization slug; omit when only one exists")

	return cmd
}

func newAddProviderCmd(env Env, flags *globalFlags) *cobra.Command {
	var (
		slug, name, issuer, clientID, orgSlug string
		scopes, defaultRole                   string
		linkByEmail, noProvision              bool
	)

	cmd := &cobra.Command{
		Use:   "add-provider",
		Short: "Configure an OpenID Connect identity provider",
		Long: `Configure single sign-on for an organization.

This exists because the HTTP endpoint that does the same thing requires an
administrator to be logged in, and an organization that wants SSO from the
outset has nobody who can be.

The client secret is read from PIVOT_OIDC_CLIENT_SECRET rather than a flag: a
secret passed as an argument lands in the shell history and in the process
list. Leave it unset for a public client, which is safe because PKCE is always
used.

--link-by-email is off by default and should stay off outside a migration. It
adopts an existing local account whose verified address matches on first
login, which is how an organization moves its users onto SSO -- and also how a
recycled address becomes an account takeover once the migration is done.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			for flag, value := range map[string]string{
				"--slug": slug, "--name": name, "--issuer": issuer, "--client-id": clientID,
			} {
				if value == "" {
					return fmt.Errorf("%s is required", flag)
				}
			}

			if defaultRole != "" && !authz.IsBuiltinRole(authz.Relation(defaultRole)) {
				return validRole(defaultRole)
			}

			db, repos, err := openRepos(cmd, env, flags)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			orgID, orgName, err := resolveOrg(cmd, repos, orgSlug)
			if err != nil {
				return err
			}

			scope, err := tenant.NewSystemScope(orgID)
			if err != nil {
				return err
			}

			ctx := tenant.WithScope(cmd.Context(), scope)

			lookup := env.Lookup
			if lookup == nil {
				lookup = os.LookupEnv
			}

			secret, _ := lookup("PIVOT_OIDC_CLIENT_SECRET")

			provider, err := repos.IdentityProviders.Create(ctx, repo.CreateIdentityProvider{
				Slug:          slug,
				Name:          name,
				Issuer:        strings.TrimRight(issuer, "/"),
				ClientID:      clientID,
				ClientSecret:  secret,
				Scopes:        scopes,
				IsEnabled:     true,
				AutoProvision: !noProvision,
				LinkByEmail:   linkByEmail,
				DefaultRole:   defaultRole,
			})
			if err != nil {
				if errors.Is(err, repo.ErrDuplicate) {
					return fmt.Errorf("a provider with slug %q already exists in %q", slug, orgName)
				}

				return err
			}

			fmt.Fprintf(env.Stdout, "Configured %s (%s) in organization %s.\n",
				provider.Name, provider.Slug, orgName)
			fmt.Fprintf(env.Stdout, "Register this redirect URI with the provider:\n  %s\n",
				"<your base URL>/api/v1/auth/oidc/"+provider.Slug+"/callback")

			if secret == "" {
				fmt.Fprintln(env.Stderr,
					"note: no client secret set; this is a public client relying on PKCE")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&slug, "slug", "", "URL segment for this provider (required)")
	cmd.Flags().StringVar(&name, "name", "", "Display name shown on the login page (required)")
	cmd.Flags().StringVar(&issuer, "issuer", "", "OIDC issuer URL (required)")
	cmd.Flags().StringVar(&clientID, "client-id", "", "OAuth client ID (required)")
	cmd.Flags().StringVar(&orgSlug, "org", "", "Organization slug; omit when only one exists")
	cmd.Flags().StringVar(&scopes, "scopes", "openid profile email", "Space-separated scopes")
	cmd.Flags().StringVar(&defaultRole, "default-role", "viewer", "Role granted to provisioned users")
	cmd.Flags().BoolVar(&linkByEmail, "link-by-email", false,
		"Adopt an existing account with a matching verified address on first login")
	cmd.Flags().BoolVar(&noProvision, "no-provision", false,
		"Refuse unknown identities rather than creating accounts for them")

	return cmd
}
