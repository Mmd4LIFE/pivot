package repo

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/store/dbtypes"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
)

// EntityConnection names a connection in a [ChangeEvent].
//
// Exported because the result cache subscribes to these events to know when a
// connection changed underneath it, and a subscriber matching on the literal
// string would keep compiling after this one was renamed.
const EntityConnection = "connection"

const entityConnection = EntityConnection

// ConnectionRepo manages an organization's data sources.
//
// The password is sealed on the way in and opened on the way out by the
// encrypting decorator in secrets.go, so nothing here mentions encryption:
// above that layer a password is a string, and the fact that it is stored as
// an envelope is a property of storage.
type ConnectionRepo struct {
	base
}

// CreateConnection describes a new data source.
type CreateConnection struct {
	Slug        string
	Name        string
	Kind        string
	Description string

	Host     string
	Port     int64
	Database string
	Username string
	Password string
	SSLMode  string
	Options  dbtypes.JSON

	// Zero means "use the instance default". A connection created before a
	// limit existed should not pin itself to whatever that limit was on the
	// day it was created.
	MaxOpenConns        int64
	MaxRows             int64
	QueryTimeoutSeconds int64

	IsEnabled bool
}

// Create adds a connection to the caller's organization.
func (r *ConnectionRepo) Create(ctx context.Context, in CreateConnection) (model.Connection, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.Connection{}, err
	}

	options := in.Options
	if len(options) == 0 {
		options = dbtypes.JSON("{}")
	}

	conn, err := r.q.CreateConnection(ctx, model.CreateConnectionParams{
		ID:                  newID(),
		OrgID:               s.OrgID(),
		Slug:                in.Slug,
		Name:                in.Name,
		Kind:                in.Kind,
		Description:         in.Description,
		Host:                in.Host,
		Port:                in.Port,
		Database:            in.Database,
		Username:            in.Username,
		Password:            in.Password,
		SslMode:             in.SSLMode,
		Options:             options,
		MaxOpenConns:        in.MaxOpenConns,
		MaxRows:             in.MaxRows,
		QueryTimeoutSeconds: in.QueryTimeoutSeconds,
		IsEnabled:           dbtypes.Bool(in.IsEnabled),
		CreatedBy:           s.ActorID(),
		UpdatedBy:           s.ActorID(),
	})
	if err != nil {
		return model.Connection{}, translate(err)
	}

	r.emit(ctx, ChangeCreated, entityConnection, conn.ID, s.OrgID(), s.ActorID())

	return conn, nil
}

// Get returns one connection.
func (r *ConnectionRepo) Get(ctx context.Context, id uuid.UUID) (model.Connection, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.Connection{}, err
	}

	conn, err := r.q.GetConnection(ctx, model.GetConnectionParams{ID: id, OrgID: s.OrgID()})
	if err != nil {
		return model.Connection{}, translate(err)
	}

	return conn, nil
}

// GetBySlug returns a connection by its URL segment.
func (r *ConnectionRepo) GetBySlug(ctx context.Context, slug string) (model.Connection, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.Connection{}, err
	}

	conn, err := r.q.GetConnectionBySlug(ctx,
		model.GetConnectionBySlugParams{OrgID: s.OrgID(), Slug: slug})
	if err != nil {
		return model.Connection{}, translate(err)
	}

	return conn, nil
}

// List returns every connection in the caller's organization.
func (r *ConnectionRepo) List(ctx context.Context) ([]model.Connection, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return nil, err
	}

	conns, err := r.q.ListConnections(ctx, s.OrgID())
	if err != nil {
		return nil, translate(err)
	}

	return conns, nil
}

// Count returns the number of connections in the caller's organization.
func (r *ConnectionRepo) Count(ctx context.Context) (int64, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return 0, err
	}

	n, err := r.q.CountConnections(ctx, s.OrgID())

	return n, translate(err)
}

// UpdateConnection describes a change.
type UpdateConnection struct {
	ID          uuid.UUID
	Slug        string
	Name        string
	Description string

	Host     string
	Port     int64
	Database string
	Username string

	// Password is written as given. A caller that does not want to change it
	// reads the connection first and passes back what it got -- which works
	// because reads decrypt, so this is a plaintext round trip rather than a
	// re-encryption of ciphertext.
	Password string

	SSLMode string
	Options dbtypes.JSON

	MaxOpenConns        int64
	MaxRows             int64
	QueryTimeoutSeconds int64

	IsEnabled bool
	Version   int64
}

// Update modifies a connection.
func (r *ConnectionRepo) Update(ctx context.Context, in UpdateConnection) (model.Connection, error) {
	s, err := r.scope(ctx)
	if err != nil {
		return model.Connection{}, err
	}

	options := in.Options
	if len(options) == 0 {
		options = dbtypes.JSON("{}")
	}

	conn, err := r.q.UpdateConnection(ctx, model.UpdateConnectionParams{
		Slug:                in.Slug,
		Name:                in.Name,
		Description:         in.Description,
		Host:                in.Host,
		Port:                in.Port,
		Database:            in.Database,
		Username:            in.Username,
		Password:            in.Password,
		SslMode:             in.SSLMode,
		Options:             options,
		MaxOpenConns:        in.MaxOpenConns,
		MaxRows:             in.MaxRows,
		QueryTimeoutSeconds: in.QueryTimeoutSeconds,
		IsEnabled:           dbtypes.Bool(in.IsEnabled),
		UpdatedBy:           s.ActorID(),
		UpdatedAt:           r.now(),
		ID:                  in.ID,
		OrgID:               s.OrgID(),
		Version:             in.Version,
	})
	if err != nil {
		// A versioned UPDATE that matches nothing is a conflict, not a missing
		// row: the caller's version is stale, or the row is deleted.
		if translated := translate(err); errors.Is(translated, ErrNotFound) {
			return model.Connection{}, ErrConflict
		} else if translated != nil {
			return model.Connection{}, translated
		}
	}

	r.emit(ctx, ChangeUpdated, entityConnection, conn.ID, s.OrgID(), s.ActorID())

	return conn, nil
}

// RecordTest stores the outcome of a connection test.
//
// Not a versioned update and not a change event: it is an observation about
// the world rather than something somebody did. Bumping the version here would
// mean a background health check invalidates the form an administrator has
// open, and emitting a change event would make every test look like an edit in
// an audit log.
func (r *ConnectionRepo) RecordTest(ctx context.Context, id uuid.UUID, testErr error) error {
	s, err := r.scope(ctx)
	if err != nil {
		return err
	}

	message := ""
	if testErr != nil {
		message = testErr.Error()
	}

	n, uerr := r.q.RecordConnectionTest(ctx, model.RecordConnectionTestParams{
		LastTestedAt:  dbtypes.NewNullTime(r.now().Time),
		LastTestOk:    dbtypes.Bool(testErr == nil),
		LastTestError: message,
		ID:            id,
		OrgID:         s.OrgID(),
	})

	return affectedOrNotFound(n, uerr)
}

// SoftDelete marks a connection deleted.
func (r *ConnectionRepo) SoftDelete(ctx context.Context, id uuid.UUID) error {
	s, err := r.scope(ctx)
	if err != nil {
		return err
	}

	n, derr := r.q.SoftDeleteConnection(ctx, model.SoftDeleteConnectionParams{
		DeletedAt: dbtypes.NewNullTime(r.now().Time),
		UpdatedBy: s.ActorID(),
		ID:        id,
		OrgID:     s.OrgID(),
	})

	if serr := affectedOrNotFound(n, derr); serr != nil {
		return serr
	}

	r.emit(ctx, ChangeDeleted, entityConnection, id, s.OrgID(), s.ActorID())

	return nil
}
