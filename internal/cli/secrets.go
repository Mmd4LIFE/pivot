package cli

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/Mmd4LIFE/pivot/internal/config"
	"github.com/Mmd4LIFE/pivot/internal/logging"
	"github.com/Mmd4LIFE/pivot/internal/secrets"
	"github.com/Mmd4LIFE/pivot/internal/store"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

// resolveSecrets assembles the keyring for a command.
//
// One function, so every entry point agrees on where the key comes from. The
// default key file is derived from the database path, because that is the only
// location a zero-config instance knows about.
func resolveSecrets(cfg *config.Config, generate bool, log *slog.Logger) (*secrets.Resolved, error) {
	file := cfg.Secrets.KeyFile
	if file == "" {
		path, _ := store.SQLitePath(cfg.Database.URL)
		file = secrets.DefaultKeyFile(path)
	}

	return secrets.Resolve(secrets.Options{
		Key:          cfg.Secrets.Key,
		File:         file,
		PreviousKeys: cfg.Secrets.PreviousKeys,
		Generate:     generate,
	}, log)
}

// newSecretsCmd builds `pivot secrets`.
func newSecretsCmd(env Env, flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage the encryption of stored secrets",
		Long: `Inspect and re-encrypt the secrets Pivot stores.

Pivot encrypts the values that must not be readable from the database alone --
today an OIDC client secret -- with a master key held outside it. These
commands report what is stored and move it onto the current key.

The key itself is never printed by anything but generate-key.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}

	cmd.AddCommand(
		newSecretsStatusCmd(env, flags),
		newSecretsRewrapCmd(env, flags),
		newSecretsGenerateKeyCmd(env),
	)

	return cmd
}

// newSecretsStatusCmd builds `pivot secrets status`.
func newSecretsStatusCmd(env Env, flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report which stored secrets are encrypted, and with which key",
		Long: `Report the state of every stored secret.

Says how many are sealed with the current key, how many with a key that has
been rotated away from, and how many are still plaintext -- which is what an
instance upgraded from a version before encryption existed has until
` + "`pivot secrets rewrap`" + ` has been through them.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			state, cleanup, err := openSecretState(cmd, env, flags, false)
			if err != nil {
				return err
			}

			defer cleanup()

			stored, err := state.storedSecrets(cmd.Context())
			if err != nil {
				return err
			}

			var current, stale, plain int

			for _, row := range stored {
				switch {
				case !secrets.IsEnvelope(row.secret):
					plain++
				case state.ring.NeedsRewrap(row.secret):
					stale++
				default:
					current++
				}
			}

			fmt.Fprintf(env.Stdout, "\nKey %s, from the %s.\n\n",
				state.ring.PrimaryID(), state.source)
			fmt.Fprintf(env.Stdout, "  %3d encrypted with the current key\n", current)
			fmt.Fprintf(env.Stdout, "  %3d encrypted with an older key\n", stale)
			fmt.Fprintf(env.Stdout, "  %3d not encrypted\n\n", plain)

			if stale+plain > 0 {
				fmt.Fprintln(env.Stdout,
					"Run `pivot secrets rewrap` to move them onto the current key.")
			}

			return nil
		},
	}
}

// newSecretsRewrapCmd builds `pivot secrets rewrap`.
func newSecretsRewrapCmd(env Env, flags *globalFlags) *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "rewrap",
		Short: "Re-encrypt every stored secret with the current key",
		Long: `Re-encrypt every stored secret with the key this instance is configured with.

This is both the migration and the rotation. After upgrading from a version
without encryption it seals the plaintext secrets; after changing the key it
moves everything off the old one, which is what lets the old key finally be
removed from secrets.previousKeys.

Reading a secret needs the key that sealed it, so rotate in two steps: make the
new key primary with the old one still in previousKeys, run this, and only then
drop the old key. Dropping it first makes every secret unreadable, permanently.

Safe to run repeatedly.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			state, cleanup, err := openSecretState(cmd, env, flags, true)
			if err != nil {
				return err
			}

			defer cleanup()

			stored, err := state.storedSecrets(cmd.Context())
			if err != nil {
				return err
			}

			var done int

			for _, row := range stored {
				if !state.ring.NeedsRewrap(row.secret) {
					continue
				}

				done++

				if dryRun {
					fmt.Fprintf(env.Stdout, "would rewrap identity provider %q (%s)\n",
						row.provider.Slug, describeState(row.secret))

					continue
				}

				if rerr := state.rewrap(cmd.Context(), row); rerr != nil {
					return fmt.Errorf("rewrap identity provider %q: %w", row.provider.Slug, rerr)
				}

				fmt.Fprintf(env.Stdout, "rewrapped identity provider %q\n", row.provider.Slug)
			}

			switch {
			case done == 0:
				fmt.Fprintln(env.Stdout, "Every stored secret is already on the current key.")
			case dryRun:
				fmt.Fprintf(env.Stdout, "\n%d secret(s) would be rewrapped.\n", done)
			default:
				fmt.Fprintf(env.Stdout, "\n%d secret(s) rewrapped onto key %s.\n",
					done, state.ring.PrimaryID())
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Report what would change without changing it")

	return cmd
}

// newSecretsGenerateKeyCmd builds `pivot secrets generate-key`.
func newSecretsGenerateKeyCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "generate-key",
		Short: "Print a new master key",
		Long: `Print a new master key, base64-encoded.

For provisioning: put it in PIVOT_SECRETS_KEY or in a key file. It is printed
rather than stored, because a command that wrote it somewhere would have to
decide where, and deciding where is the reason to supply a key at all.

Generating a key rotates nothing. See ` + "`pivot secrets rewrap`" + `.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			key, err := secrets.GenerateKey()
			if err != nil {
				return err
			}

			fmt.Fprintln(env.Stdout, key.Encode())

			return nil
		},
	}
}

/*
secretState is the pair of views these commands need.

`sealed` reads the columns exactly as the database holds them, which is the
question "what is stored". `live` reads and writes through the keyring, which
is the question "what does it mean". Rewrapping is going from one to the other,
so both have to exist at once.
*/
type secretState struct {
	ring   *secrets.Keyring
	source secrets.Source

	sealed *repo.Repositories
	live   *repo.Repositories
}

// storedRow is one secret as the database holds it.
type storedRow struct {
	orgID    string
	provider model.IdentityProvider
	secret   string
}

// openSecretState connects and builds both views.
func openSecretState(
	cmd *cobra.Command, env Env, flags *globalFlags, generate bool,
) (*secretState, func(), error) {
	res, err := loadConfig(cmd, env, flags)
	if err != nil {
		return nil, nil, err
	}

	log := logging.New(res.Config.Log, env.Stderr)

	db, err := store.Open(cmd.Context(), res.Config.Database, log)
	if err != nil {
		return nil, nil, err
	}

	cleanup := func() { _ = db.Close() }

	resolved, err := resolveSecrets(res.Config, generate, log)
	if err != nil {
		cleanup()

		return nil, nil, err
	}

	return &secretState{
		ring:   resolved.Keyring,
		source: resolved.Source,
		// Plaintext here means "do not transform", which is what reading the
		// raw column requires. It is the one place in the product that wants
		// that, and it is named rather than implied.
		sealed: repo.New(db, repo.WithSecrets(secrets.Plaintext())),
		live:   repo.New(db, repo.WithSecrets(resolved.Keyring)),
	}, cleanup, nil
}

// storedSecrets lists every stored secret across every organization.
func (s *secretState) storedSecrets(ctx context.Context) ([]storedRow, error) {
	orgs, err := s.sealed.System().ListOrganizations(ctx, 1000, 0)
	if err != nil {
		return nil, fmt.Errorf("list organizations: %w", err)
	}

	var rows []storedRow

	for _, org := range orgs {
		scope, serr := tenant.NewSystemScope(org.ID)
		if serr != nil {
			return nil, serr
		}

		scoped := tenant.WithScope(ctx, scope)

		providers, perr := s.sealed.IdentityProviders.List(scoped)
		if perr != nil {
			return nil, fmt.Errorf("list identity providers for %s: %w", org.Slug, perr)
		}

		for _, provider := range providers {
			if provider.ClientSecret == "" {
				continue
			}

			rows = append(rows, storedRow{
				orgID: org.ID.String(), provider: provider, secret: provider.ClientSecret,
			})
		}
	}

	return rows, nil
}

// rewrap decrypts with the full keyring and writes back through the primary.
func (s *secretState) rewrap(ctx context.Context, row storedRow) error {
	plaintext, err := s.ring.Decrypt(repo.PurposeClientSecret, row.secret)
	if err != nil {
		return err
	}

	scope, err := tenant.NewSystemScope(row.provider.OrgID)
	if err != nil {
		return err
	}

	p := row.provider

	_, err = s.live.IdentityProviders.Update(tenant.WithScope(ctx, scope),
		repo.UpdateIdentityProvider{
			ID:            p.ID,
			Slug:          p.Slug,
			Name:          p.Name,
			Issuer:        p.Issuer,
			ClientID:      p.ClientID,
			ClientSecret:  plaintext,
			Scopes:        p.Scopes,
			IsEnabled:     bool(p.IsEnabled),
			AutoProvision: bool(p.AutoProvision),
			LinkByEmail:   bool(p.LinkByEmail),
			DefaultRole:   p.DefaultRole,
			ClaimMapping:  p.ClaimMapping,
			Version:       p.Version,
		})

	return err
}

// describeState says why a row needs rewrapping, for the dry run.
func describeState(stored string) string {
	if !secrets.IsEnvelope(stored) {
		return "plaintext"
	}

	return "an older key"
}
