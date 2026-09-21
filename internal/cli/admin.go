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
	"github.com/Mmd4LIFE/pivot/internal/config"
	"github.com/Mmd4LIFE/pivot/internal/logging"
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

			user, err := repos.Users.Create(ctx, repo.CreateUser{
				Email:        email,
				Name:         name,
				PasswordHash: hash,
				IsActive:     true,
			})
			if err != nil {
				if errors.Is(err, repo.ErrDuplicate) {
					return fmt.Errorf("a user with email %q already exists in %q", email, slug)
				}

				return err
			}

			fmt.Fprintf(env.Stdout, "Created user %s (%s) in organization %s.\n",
				user.Email, user.ID, slug)

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
func slugify(in string) string {
	out := make([]rune, 0, len(in))
	lastDash := true

	for _, r := range strings.ToLower(in) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
			lastDash = false
		default:
			if !lastDash {
				out = append(out, '-')
				lastDash = true
			}
		}
	}

	if n := len(out); n > 0 && out[n-1] == '-' {
		out = out[:n-1]
	}

	if len(out) == 0 {
		return "org"
	}

	return string(out)
}
