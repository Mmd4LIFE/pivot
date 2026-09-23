// Package setup owns the first run: the moment a Pivot has a database, a
// listener, and nobody who can log in.
//
// It exists as a package rather than a handler because two callers provision
// accounts — `pivot admin create-user` and the browser — and the rule that
// matters is the same for both: the first user in an organization becomes its
// administrator, and only the first. Written twice, that rule eventually
// disagrees with itself, and the disagreement is a privilege escalation.
package setup

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/auth"
	"github.com/Mmd4LIFE/pivot/internal/authz"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

// Errors the caller has to distinguish.
var (
	// ErrAlreadyInitialized means somebody has already claimed this instance.
	// It is permanent: there is no path back to an unclaimed Pivot short of a
	// new database.
	ErrAlreadyInitialized = errors.New("setup: this Pivot already has an administrator")

	// ErrTokenRequired means no token was presented.
	ErrTokenRequired = errors.New("setup: a setup token is required")

	// ErrTokenInvalid means the token presented was not this instance's.
	ErrTokenInvalid = errors.New("setup: the setup token is not correct")
)

// Status describes whether this Pivot has been claimed.
type Status struct {
	// Initialized is true once any organization exists. The browser uses it to
	// decide between the setup page and the login page.
	Initialized bool
}

// Request is what somebody needs to supply to claim an instance.
type Request struct {
	Organization string
	Name         string
	Email        string
	Password     string

	// Token is the one printed in the server's startup banner.
	Token string
}

// Result is the account that was created.
type Result struct {
	OrgID   uuid.UUID
	OrgSlug string
	UserID  uuid.UUID
	Email   string
}

// Service answers whether an instance is claimed, and claims it.
type Service struct {
	repos *repo.Repositories
	token string

	// mu serializes Initialize.
	//
	// Two browsers posting the setup form at the same moment would otherwise
	// both read "no organizations", both create one, and produce two
	// administrators of two tenants on an instance meant to have one. The
	// repository layer has no transactions to lean on, so the exclusion is
	// here, where the check and the write are.
	//
	// This is per-process, which is the honest scope: a second Pivot pointed at
	// the same fresh Postgres could still race it. That window is one HTTP
	// request wide, on an instance nobody has claimed yet, and closing it
	// properly needs a database-level lock this layer does not have. Written
	// down rather than hidden.
	mu sync.Mutex
}

// NewService builds the service. An empty token means setup is unprotected.
func NewService(repos *repo.Repositories, token string) *Service {
	return &Service{repos: repos, token: token}
}

// Status reports whether this Pivot has been claimed.
//
// Keyed on organizations rather than users, because an organization with no
// users is a half-finished setup that nobody can log into or finish. Counting
// users would call that instance claimed and leave it permanently unusable.
func (s *Service) Status(ctx context.Context) (Status, error) {
	n, err := s.repos.System().CountOrganizations(ctx)
	if err != nil {
		return Status{}, fmt.Errorf("count organizations: %w", err)
	}

	return Status{Initialized: n > 0}, nil
}

// Initialize creates the first organization and its administrator.
//
// It refuses once anything exists. The check happens inside the lock and again
// against the database, rather than being cached at startup: an instance that
// decided at boot that it was unclaimed would stay claimable for as long as it
// ran.
func (s *Service) Initialize(ctx context.Context, in Request) (Result, error) {
	if err := s.checkToken(in.Token); err != nil {
		return Result{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	status, err := s.Status(ctx)
	if err != nil {
		return Result{}, err
	}

	if status.Initialized {
		return Result{}, ErrAlreadyInitialized
	}

	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return Result{}, err
	}

	org, err := s.repos.System().CreateOrganization(ctx, repo.CreateOrganization{
		Name: strings.TrimSpace(in.Organization),
		Slug: Slugify(in.Organization),
	})
	if err != nil {
		return Result{}, fmt.Errorf("create organization: %w", err)
	}

	// No actor: nobody exists yet to have done this. A scope that invented one
	// would put a user ID in created_by that never existed.
	scope, err := tenant.NewSystemScope(org.ID)
	if err != nil {
		return Result{}, err
	}

	user, _, err := CreateUser(tenant.WithScope(ctx, scope), s.repos, org.ID, CreateUserRequest{
		Email:        in.Email,
		Name:         in.Name,
		PasswordHash: hash,
	})
	if err != nil {
		// The organization is left behind deliberately. Deleting it would be a
		// second write that can also fail, and Status keys on organizations --
		// so a half-finished setup stays claimable and the next attempt, with
		// a valid email, walks the same path. See Status.
		return Result{}, fmt.Errorf("create the first user: %w", err)
	}

	return Result{
		OrgID:   org.ID,
		OrgSlug: org.Slug,
		UserID:  user.ID,
		Email:   user.Email,
	}, nil
}

// TokenRequired reports whether this instance demands a setup token.
func (s *Service) TokenRequired() bool { return s.token != "" }

// checkToken compares the presented token with this instance's.
func (s *Service) checkToken(presented string) error {
	if s.token == "" {
		return nil
	}

	if presented == "" {
		return ErrTokenRequired
	}

	// Constant time, because the comparison is against a secret and a timing
	// oracle on it is a way to recover it a byte at a time.
	if subtle.ConstantTimeCompare([]byte(presented), []byte(s.token)) != 1 {
		return ErrTokenInvalid
	}

	return nil
}

// CreateUserRequest describes an account to provision.
type CreateUserRequest struct {
	Email string
	Name  string

	// PasswordHash is already hashed. This package never sees a plaintext
	// password it did not hash itself, so no caller can pass one through by
	// mistake.
	PasswordHash string
}

// CreateUser creates a user and makes them an administrator if they are the
// first in their organization.
//
// The caller supplies a context already carrying the organization's scope.
// Returns the user and whether the admin role was granted.
//
// The first-user rule lives here, in one place, because both the CLI and the
// browser provision accounts and a rule about who becomes an administrator
// cannot be allowed to exist in two versions.
func CreateUser(
	ctx context.Context, repos *repo.Repositories, orgID uuid.UUID, in CreateUserRequest,
) (user model.User, granted bool, err error) {
	// Whether this is the first user has to be decided before creating them,
	// or the answer is always "no".
	existing, err := repos.Users.Count(ctx)
	if err != nil {
		return model.User{}, false, err
	}

	created, err := repos.Users.Create(ctx, repo.CreateUser{
		Email:        in.Email,
		Name:         in.Name,
		PasswordHash: in.PasswordHash,
		IsActive:     true,
	})
	if err != nil {
		return model.User{}, false, err
	}

	// Without this a fresh install has nobody who can grant a role, so nobody
	// can ever be granted one -- the instance is complete and unusable.
	// Granting it only to the first user keeps it from being a standing
	// privilege escalation: the second user gets nothing.
	if existing > 0 {
		return created, false, nil
	}

	if gerr := repos.Roles.Grant(ctx, repo.GrantRole{
		SubjectType: "user",
		SubjectID:   created.ID,
		Relation:    string(authz.RelationAdmin),
		ObjectType:  string(authz.TypeOrganization),
		ObjectID:    orgID,
	}); gerr != nil {
		return model.User{}, false, fmt.Errorf("grant admin to the first user: %w", gerr)
	}

	return created, true, nil
}

// NewToken generates a setup token.
//
// 32 bytes from crypto/rand, URL-safe so it survives being pasted into a
// browser and a shell alike. It is not a password and is never stored: it
// lives for one process, protecting one action, and stops mattering the moment
// somebody claims the instance.
func NewToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate setup token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Slugify turns a display name into a URL-safe slug.
func Slugify(in string) string {
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
