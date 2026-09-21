package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/config"
	"github.com/Mmd4LIFE/pivot/internal/logging"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

// Errors a caller may branch on.
//
// ErrInvalidCredentials is deliberately the only outcome for every failed
// login: unknown account, wrong password, and disabled account all return it.
// Distinguishing them tells an attacker which addresses are real.
var (
	ErrInvalidCredentials = errors.New("auth: invalid credentials")

	// ErrAccountLocked is returned after too many consecutive failures. It is
	// distinguishable on purpose — the alternative is a user who cannot log in
	// with the right password and is told nothing.
	ErrAccountLocked = errors.New("auth: account temporarily locked")

	// ErrSessionInvalid covers unknown, expired and revoked sessions alike.
	ErrSessionInvalid = errors.New("auth: session is not valid")
)

// Policy is the tunable part of authentication.
type Policy struct {
	// IdleTimeout ends a session that has not been used. Pushed forward on use.
	IdleTimeout time.Duration

	// AbsoluteTimeout is a hard cap, never extended. Without it a stolen token
	// stays valid indefinitely as long as the thief keeps using it.
	AbsoluteTimeout time.Duration

	// MaxFailedAttempts before a lockout is applied.
	MaxFailedAttempts int64

	// LockoutDuration is the base window. It doubles with each further
	// lockout, up to MaxLockoutDuration — a fixed window is trivially waited
	// out by a patient attacker, while an ever-growing one locks a real user
	// out permanently.
	LockoutDuration    time.Duration
	MaxLockoutDuration time.Duration

	// TouchInterval throttles the sliding-expiry write. Updating the session
	// row on every request turns a read-mostly workload into a write-heavy
	// one, which on SQLite means serializing behind a single writer.
	TouchInterval time.Duration
}

// DefaultPolicy is a reasonable starting point.
func DefaultPolicy() Policy {
	return Policy{
		IdleTimeout:        8 * time.Hour,
		AbsoluteTimeout:    30 * 24 * time.Hour,
		MaxFailedAttempts:  10,
		LockoutDuration:    1 * time.Minute,
		MaxLockoutDuration: 1 * time.Hour,
		TouchInterval:      5 * time.Minute,
	}
}

// PolicyFrom builds a policy from configuration.
//
// TouchInterval is deliberately absent from the configuration surface: it is
// a write-amplification tuning knob with no security meaning, and every
// setting an operator can reach is one they can get wrong.
func PolicyFrom(cfg config.AuthConfig) Policy {
	p := DefaultPolicy()

	p.IdleTimeout = cfg.SessionIdleTimeout.Duration()
	p.AbsoluteTimeout = cfg.SessionAbsoluteTimeout.Duration()
	p.MaxFailedAttempts = int64(cfg.MaxFailedAttempts)
	p.LockoutDuration = cfg.LockoutDuration.Duration()
	p.MaxLockoutDuration = cfg.LockoutMaxDuration.Duration()

	return p
}

// Service performs authentication.
type Service struct {
	repos  *repo.Repositories
	policy Policy
	log    *slog.Logger

	// now is injectable so tests can advance time without sleeping.
	now func() time.Time
}

// NewService builds an authentication service.
func NewService(repos *repo.Repositories, policy Policy, log *slog.Logger) *Service {
	return &Service{repos: repos, policy: policy, log: log, now: time.Now}
}

// SetClock replaces the time source. For tests only.
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// Credentials identify a login attempt.
type Credentials struct {
	OrgID    uuid.UUID
	Email    string
	Password string

	IP        string
	UserAgent string
}

// LoginResult is a successful authentication.
type LoginResult struct {
	// Token is the raw session token. It is returned exactly once, here, and
	// never stored or logged.
	Token string

	Session model.Session
	User    model.User
}

// Login authenticates a user and opens a session.
//
// Every failure path costs roughly the same: a nonexistent account still pays
// for an Argon2 verification against a dummy hash, and all of them return
// ErrInvalidCredentials. Matching the response body but not the timing leaves
// the enumeration oracle wide open.
func (s *Service) Login(ctx context.Context, in Credentials) (*LoginResult, error) {
	now := s.now()
	email := repo.NormalizeEmail(in.Email)
	sys := s.repos.System()

	// A locked account is refused before any password work, so a lockout also
	// serves as a brake on the Argon2 cost an attacker can force us to spend.
	if until, locked, err := sys.LockedUntil(ctx, in.OrgID, email, now); err != nil {
		return nil, fmt.Errorf("auth: check lockout: %w", err)
	} else if locked {
		s.log.Warn("login refused: account locked",
			slog.String("email", email),
			slog.String("until", until.Format(time.RFC3339)),
		)

		return nil, ErrAccountLocked
	}

	// The user lookup needs a scope, and login has none yet. This is the one
	// place a scope is minted from a claimed organization rather than from an
	// authenticated identity — the password check below is what validates it.
	scope, err := tenant.NewSystemScope(in.OrgID)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	scoped := tenant.WithScope(ctx, scope)

	user, err := s.repos.Users.GetByEmail(scoped, email)
	if err != nil {
		if !errors.Is(err, repo.ErrNotFound) {
			return nil, fmt.Errorf("auth: look up user: %w", err)
		}

		// No such account. Spend the same time a real verification would, then
		// record the failure so that guessing addresses is rate limited too.
		SpendVerifyTime(in.Password)
		s.recordFailure(ctx, in.OrgID, email, now)

		return nil, ErrInvalidCredentials
	}

	// A user with no password cannot log in this way — an SSO-only account,
	// or one provisioned before a password was set. Still spend the time.
	if !user.PasswordHash.Valid || user.PasswordHash.String == "" {
		SpendVerifyTime(in.Password)
		s.recordFailure(ctx, in.OrgID, email, now)

		return nil, ErrInvalidCredentials
	}

	ok, err := VerifyPassword(in.Password, user.PasswordHash.String)
	if err != nil {
		// A malformed stored hash is an operational problem, not a user error.
		// It is logged and reported as invalid credentials, because telling the
		// caller "your hash is corrupt" helps nobody but an attacker.
		s.log.Error("stored password hash is unusable",
			slog.String("user_id", user.ID.String()),
			logging.Err(err),
		)
		s.recordFailure(ctx, in.OrgID, email, now)

		return nil, ErrInvalidCredentials
	}

	if !ok {
		s.recordFailure(ctx, in.OrgID, email, now)

		return nil, ErrInvalidCredentials
	}

	// An inactive account gets the same answer as a wrong password. Saying
	// "this account is disabled" confirms the address exists.
	if !user.IsActive.Bool() {
		s.recordFailure(ctx, in.OrgID, email, now)

		return nil, ErrInvalidCredentials
	}

	if cerr := sys.ClearLoginAttempts(ctx, in.OrgID, email); cerr != nil {
		// Not fatal: the credentials were correct. Failing the login here
		// would deny a legitimate user over bookkeeping.
		s.log.Warn("could not clear login attempts", logging.Err(cerr))
	}

	return s.StartSession(ctx, user, in.IP, in.UserAgent)
}

// StartSession issues a session for an already-authenticated user.
//
// This is the second half of [Login], factored out so that single sign-on
// produces a session by the same code path rather than a parallel one. Two
// ways to mint a session is two places for the expiry policy to drift, and
// only one of them would be covered by the tests that matter.
//
// It does NOT authenticate. Every caller must have established identity
// already — a password check here, a verified ID token in internal/oidc —
// which is why it is named for what it does rather than for what it looks
// like.
func (s *Service) StartSession(
	ctx context.Context, user model.User, ip, userAgent string,
) (*LoginResult, error) {
	now := s.now()

	// A disabled account cannot hold a session, whatever route asked for one.
	// The password path checks this too; repeating it here means a new caller
	// cannot skip it by not knowing about it.
	if !user.IsActive.Bool() {
		return nil, ErrInvalidCredentials
	}

	scope, err := tenant.NewSystemScope(user.OrgID)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	scoped := tenant.WithScope(ctx, scope)

	token, err := NewToken()
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	session, err := s.repos.System().CreateSession(ctx, repo.CreateSession{
		OrgID:             user.OrgID,
		UserID:            user.ID,
		TokenHash:         HashToken(token),
		ExpiresAt:         now.Add(s.policy.IdleTimeout),
		AbsoluteExpiresAt: now.Add(s.policy.AbsoluteTimeout),
		IP:                ip,
		UserAgent:         userAgent,
	})
	if err != nil {
		return nil, fmt.Errorf("auth: create session: %w", err)
	}

	if rerr := s.repos.Users.RecordLogin(scoped, user.ID); rerr != nil {
		s.log.Warn("could not record login timestamp", logging.Err(rerr))
	}

	s.log.Info("session started",
		slog.String("user_id", user.ID.String()),
		slog.String("org_id", user.OrgID.String()),
		slog.String("session_id", session.ID.String()),
	)

	return &LoginResult{Token: token, Session: session, User: user}, nil
}

// recordFailure increments the failure count and applies a lockout when the
// threshold is crossed.
//
// Failures here are logged but never returned: a bookkeeping error must not
// turn a failed login into a different-looking failed login, which would be
// its own oracle.
func (s *Service) recordFailure(ctx context.Context, orgID uuid.UUID, email string, now time.Time) {
	sys := s.repos.System()

	attempt, err := sys.RecordFailedLogin(ctx, orgID, email, now)
	if err != nil {
		s.log.Warn("could not record failed login", logging.Err(err))

		return
	}

	if attempt.FailedCount < s.policy.MaxFailedAttempts {
		return
	}

	// Each further threshold crossing doubles the window, capped. Counting
	// from the threshold means the first lockout is the base duration.
	excess := attempt.FailedCount - s.policy.MaxFailedAttempts
	lockout := s.policy.LockoutDuration

	for range excess {
		lockout *= 2
		if lockout >= s.policy.MaxLockoutDuration {
			lockout = s.policy.MaxLockoutDuration

			break
		}
	}

	if lerr := sys.LockLogin(ctx, orgID, email, now.Add(lockout)); lerr != nil {
		s.log.Warn("could not apply lockout", logging.Err(lerr))

		return
	}

	s.log.Warn("account locked after repeated failures",
		slog.String("email", email),
		slog.Int64("failures", attempt.FailedCount),
		slog.String("lockout", lockout.String()),
	)
}

// Authenticated is a resolved session.
type Authenticated struct {
	Session model.Session
	Scope   tenant.Scope
}

// Authenticate resolves a session token.
//
// Unknown, expired and revoked all return ErrSessionInvalid: the caller has no
// legitimate use for the difference, and reporting it tells a probe whether a
// token was ever real.
func (s *Service) Authenticate(ctx context.Context, token string) (*Authenticated, error) {
	if token == "" {
		return nil, ErrSessionInvalid
	}

	now := s.now()
	sys := s.repos.System()

	session, err := sys.GetSessionByTokenHash(ctx, HashToken(token))
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, ErrSessionInvalid
		}

		return nil, fmt.Errorf("auth: resolve session: %w", err)
	}

	if session.RevokedAt.Valid {
		return nil, ErrSessionInvalid
	}

	// Both expiries are checked here rather than in SQL, so the policy lives
	// in one place instead of half in a WHERE clause.
	if !session.ExpiresAt.After(now) || !session.AbsoluteExpiresAt.After(now) {
		return nil, ErrSessionInvalid
	}

	scope, err := tenant.NewScope(session.OrgID, uuid.NullUUID{UUID: session.UserID, Valid: true})
	if err != nil {
		return nil, ErrSessionInvalid
	}

	// Slide the idle expiry, but not on every request: a read-mostly workload
	// should not become write-heavy, and on SQLite that means serializing
	// behind the single writer.
	if now.Sub(session.LastSeenAt.Time) > s.policy.TouchInterval {
		newExpiry := now.Add(s.policy.IdleTimeout)
		if terr := sys.TouchSession(ctx, session.ID, now, newExpiry); terr != nil {
			// The session was revoked between resolution and touch.
			if errors.Is(terr, repo.ErrNotFound) {
				return nil, ErrSessionInvalid
			}

			s.log.Warn("could not touch session", logging.Err(terr))
		}
	}

	return &Authenticated{Session: session, Scope: scope}, nil
}

// Logout revokes a session immediately.
//
// Immediate is the whole reason sessions are server-side: the next request
// carrying this token is rejected, with no window to wait out.
func (s *Service) Logout(ctx context.Context, token string) error {
	auth, err := s.Authenticate(ctx, token)
	if err != nil {
		// Logging out an already-invalid session is a success from the
		// caller's perspective: the desired state is reached.
		if errors.Is(err, ErrSessionInvalid) {
			return nil
		}

		return err
	}

	scoped := tenant.WithScope(ctx, auth.Scope)

	if rerr := s.repos.Sessions.Revoke(scoped, auth.Session.ID); rerr != nil {
		if errors.Is(rerr, repo.ErrNotFound) {
			return nil
		}

		return fmt.Errorf("auth: revoke session: %w", rerr)
	}

	return nil
}

// SetPassword changes a user's password and ends their other sessions.
//
// Revoking is the point: a password change is usually a response to suspected
// compromise, and leaving the attacker's session alive defeats it.
func (s *Service) SetPassword(ctx context.Context, userID uuid.UUID, newPassword string) error {
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}

	if serr := s.repos.Users.SetPassword(ctx, userID, hash); serr != nil {
		return fmt.Errorf("auth: set password: %w", serr)
	}

	if _, rerr := s.repos.Sessions.RevokeAllForUser(ctx, userID); rerr != nil {
		return fmt.Errorf("auth: revoke sessions after password change: %w", rerr)
	}

	return nil
}

// Sweep removes expired sessions and stale login attempts.
//
// Part 5's scheduler does not exist yet, so this is called on startup and is
// wired to a ticker in Phase 5 alongside the rest of the background work.
func (s *Service) Sweep(ctx context.Context, retain time.Duration) (sessions, attempts int64, err error) {
	now := s.now()
	sys := s.repos.System()

	sessions, err = sys.SweepExpired(ctx, now, retain)
	if err != nil {
		return 0, 0, fmt.Errorf("auth: sweep sessions: %w", err)
	}

	attempts, err = sys.SweepLoginAttempts(ctx, now, retain)
	if err != nil {
		return sessions, 0, fmt.Errorf("auth: sweep login attempts: %w", err)
	}

	return sessions, attempts, nil
}
