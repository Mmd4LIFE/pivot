package repo

import (
	"context"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/secrets"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
)

/*
Encryption of the columns that hold secrets.

A decorator over the Querier rather than code inside each repository, for the
same reason the tracing decorator is one: it puts every secret column in a
single list that can be read in one sitting. A repository method that forgets to
encrypt is a plaintext secret nobody notices; a decorator that does not mention
a column is a visible gap in a short file.

It also keeps the repositories free of crypto. Above this layer a client secret
is a string, and the fact that it is stored sealed is a property of storage.

**When a secret column is added, it is added here.** There is no mechanism that
finds one automatically, and there is not supposed to be: an automatic one would
have to guess from a column's name, and it would be wrong in the direction that
does not fail.
*/

// Purposes bind a ciphertext to where it is stored. Authenticated, not secret:
// a value lifted out of one column cannot be pasted into another and decrypted
// there.
const (
	PurposeClientSecret       = "identity_provider.client_secret"
	PurposeConnectionPassword = "connection.password"
)

// encryptingQuerier seals secret columns on the way in and opens them on the
// way out.
type encryptingQuerier struct {
	Querier

	cipher secrets.Cipher
}

// encrypting wraps a Querier so that secret columns are stored sealed.
func encrypting(next Querier, c secrets.Cipher) Querier {
	if c == nil {
		c = secrets.Refusing()
	}

	return &encryptingQuerier{Querier: next, cipher: c}
}

func (e *encryptingQuerier) CreateIdentityProvider(
	ctx context.Context, p model.CreateIdentityProviderParams,
) (model.IdentityProvider, error) {
	sealed, err := e.cipher.Encrypt(PurposeClientSecret, p.ClientSecret)
	if err != nil {
		return model.IdentityProvider{}, err
	}

	p.ClientSecret = sealed

	row, err := e.Querier.CreateIdentityProvider(ctx, p)
	if err != nil {
		return row, err
	}

	// Opened again on the way back, so a caller gets what it passed in rather
	// than an envelope. A create that returned ciphertext would work until
	// somebody used the returned row, which is the kind of bug that surfaces
	// three layers away.
	return e.open(row)
}

func (e *encryptingQuerier) UpdateIdentityProvider(
	ctx context.Context, p model.UpdateIdentityProviderParams,
) (model.IdentityProvider, error) {
	sealed, err := e.cipher.Encrypt(PurposeClientSecret, p.ClientSecret)
	if err != nil {
		return model.IdentityProvider{}, err
	}

	p.ClientSecret = sealed

	row, err := e.Querier.UpdateIdentityProvider(ctx, p)
	if err != nil {
		return row, err
	}

	return e.open(row)
}

func (e *encryptingQuerier) GetIdentityProvider(
	ctx context.Context, p model.GetIdentityProviderParams,
) (model.IdentityProvider, error) {
	row, err := e.Querier.GetIdentityProvider(ctx, p)
	if err != nil {
		return row, err
	}

	return e.open(row)
}

func (e *encryptingQuerier) GetIdentityProviderBySlug(
	ctx context.Context, p model.GetIdentityProviderBySlugParams,
) (model.IdentityProvider, error) {
	row, err := e.Querier.GetIdentityProviderBySlug(ctx, p)
	if err != nil {
		return row, err
	}

	return e.open(row)
}

func (e *encryptingQuerier) ListIdentityProviders(
	ctx context.Context, orgID uuid.UUID,
) ([]model.IdentityProvider, error) {
	rows, err := e.Querier.ListIdentityProviders(ctx, orgID)
	if err != nil {
		return rows, err
	}

	for i, row := range rows {
		opened, oerr := e.open(row)
		if oerr != nil {
			// One unreadable secret does not make the list unreadable. An
			// administrator looking at the providers page during a botched key
			// rotation needs to see which provider is broken, and a list that
			// fails entirely tells them nothing -- so the row survives with an
			// empty secret, and the SSO flow that needs it fails on its own.
			opened = row
			opened.ClientSecret = ""
		}

		rows[i] = opened
	}

	return rows, nil
}

// --- connections ------------------------------------------------------------

func (e *encryptingQuerier) CreateConnection(
	ctx context.Context, p model.CreateConnectionParams,
) (model.Connection, error) {
	sealed, err := e.cipher.Encrypt(PurposeConnectionPassword, p.Password)
	if err != nil {
		return model.Connection{}, err
	}

	p.Password = sealed

	row, err := e.Querier.CreateConnection(ctx, p)
	if err != nil {
		return row, err
	}

	return e.openConnection(row)
}

func (e *encryptingQuerier) UpdateConnection(
	ctx context.Context, p model.UpdateConnectionParams,
) (model.Connection, error) {
	sealed, err := e.cipher.Encrypt(PurposeConnectionPassword, p.Password)
	if err != nil {
		return model.Connection{}, err
	}

	p.Password = sealed

	row, err := e.Querier.UpdateConnection(ctx, p)
	if err != nil {
		return row, err
	}

	return e.openConnection(row)
}

func (e *encryptingQuerier) GetConnection(
	ctx context.Context, p model.GetConnectionParams,
) (model.Connection, error) {
	row, err := e.Querier.GetConnection(ctx, p)
	if err != nil {
		return row, err
	}

	return e.openConnection(row)
}

func (e *encryptingQuerier) GetConnectionBySlug(
	ctx context.Context, p model.GetConnectionBySlugParams,
) (model.Connection, error) {
	row, err := e.Querier.GetConnectionBySlug(ctx, p)
	if err != nil {
		return row, err
	}

	return e.openConnection(row)
}

func (e *encryptingQuerier) ListConnections(
	ctx context.Context, orgID uuid.UUID,
) ([]model.Connection, error) {
	rows, err := e.Querier.ListConnections(ctx, orgID)
	if err != nil {
		return rows, err
	}

	for i, row := range rows {
		opened, oerr := e.openConnection(row)
		if oerr != nil {
			// One unreadable password does not make the list unreadable. An
			// administrator looking at this page during a botched key rotation
			// needs to see which connection is broken; the query that needs
			// the password fails on its own.
			opened = row
			opened.Password = ""
		}

		rows[i] = opened
	}

	return rows, nil
}

// openConnection decrypts a connection's secret columns.
func (e *encryptingQuerier) openConnection(row model.Connection) (model.Connection, error) {
	plaintext, err := e.cipher.Decrypt(PurposeConnectionPassword, row.Password)
	if err != nil {
		return model.Connection{}, err
	}

	row.Password = plaintext

	return row, nil
}

// open decrypts a row's secret columns.
func (e *encryptingQuerier) open(row model.IdentityProvider) (model.IdentityProvider, error) {
	plaintext, err := e.cipher.Decrypt(PurposeClientSecret, row.ClientSecret)
	if err != nil {
		return model.IdentityProvider{}, err
	}

	row.ClientSecret = plaintext

	return row, nil
}
