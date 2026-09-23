package auth_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Mmd4LIFE/pivot/internal/auth"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

/*
The parts of authentication that only run when something is wrong.

A lockout that does not escalate, an account that cannot use a password, a
stored hash that cannot be parsed, a password change that leaves the attacker's
session alive -- each of these is silent in the happy path and only shows up
on the day it matters.
*/

// withPolicy rebuilds the fixture's service with a policy a test can reach.
//
// The default is ten failures and a one-minute base window, which would make
// the escalation test spend most of its time counting to ten.
func (f *fixture) withPolicy(policy auth.Policy) *auth.Service {
	svc := auth.NewService(f.repos, policy, slog.New(slog.NewJSONHandler(f.logs, nil)))
	svc.SetClock(func() time.Time { return f.clock })

	return svc
}

/*
The lockout window doubles, and then stops doubling.

Both halves matter and for opposite reasons. A fixed window is waited out by
anybody patient enough to write a loop, which is everybody. An unbounded one
locks a real person out for a week because somebody else guessed at their email
address, which turns the brake into a denial of service against the user it is
supposed to protect.
*/
func TestTheLockoutWindowDoublesUpToTheCap(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, f *fixture) {
		policy := auth.DefaultPolicy()
		policy.MaxFailedAttempts = 2
		policy.LockoutDuration = time.Minute
		policy.MaxLockoutDuration = 4 * time.Minute

		svc := f.withPolicy(policy)

		fail := func() {
			_, err := svc.Login(context.Background(), auth.Credentials{
				OrgID: f.org.ID, Email: testEmail, Password: "wrong", IP: "127.0.0.1",
			})
			if !errors.Is(err, auth.ErrInvalidCredentials) && !errors.Is(err, auth.ErrAccountLocked) {
				t.Fatalf("login with a wrong password: %v", err)
			}
		}

		// lockedFor reads the remaining window straight off the attempt row,
		// which is the only way to see the duration rather than merely that
		// there is one.
		lockedFor := func() time.Duration {
			t.Helper()

			until, locked, err := f.repos.System().LockedUntil(
				context.Background(), f.org.ID, testEmail, f.clock)
			if err != nil {
				t.Fatalf("locked until: %v", err)
			}

			if !locked {
				return 0
			}

			return until.Sub(f.clock).Round(time.Second)
		}

		fail()

		if got := lockedFor(); got != 0 {
			t.Fatalf("locked after one failure of two allowed: %s", got)
		}

		// Crossing the threshold: the first lockout is the base window, not
		// the base window doubled. Counting the excess from the threshold is
		// what makes that true.
		fail()

		if got := lockedFor(); got != time.Minute {
			t.Fatalf("first lockout = %s, want 1m", got)
		}

		// Each further crossing doubles it. A locked account is refused before
		// the failure is recorded, so the clock has to move past the window
		// for the next attempt to count at all.
		for _, want := range []time.Duration{2 * time.Minute, 4 * time.Minute} {
			f.advance(lockedFor() + time.Second)
			fail()

			if got := lockedFor(); got != want {
				t.Fatalf("lockout = %s, want %s", got, want)
			}
		}

		// And then stops. Without the cap this one would be eight minutes,
		// and the one after that sixteen.
		f.advance(lockedFor() + time.Second)
		fail()

		if got := lockedFor(); got != 4*time.Minute {
			t.Errorf("lockout = %s, want it capped at 4m", got)
		}
	})
}

// A locked account is refused before any password work. The lockout is also
// the brake on how much Argon2 an anonymous caller can make this process
// spend, which it cannot be if the hash is computed first.
func TestALockedAccountIsRefusedEvenWithTheRightPassword(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, f *fixture) {
		policy := auth.DefaultPolicy()
		policy.MaxFailedAttempts = 1
		policy.LockoutDuration = time.Minute

		svc := f.withPolicy(policy)

		credentials := func(password string) auth.Credentials {
			return auth.Credentials{
				OrgID: f.org.ID, Email: testEmail, Password: password, IP: "127.0.0.1",
			}
		}

		if _, err := svc.Login(context.Background(), credentials("wrong")); !errors.Is(
			err, auth.ErrInvalidCredentials) {
			t.Fatalf("first failure: %v", err)
		}

		_, err := svc.Login(context.Background(), credentials(testPassword))

		if !errors.Is(err, auth.ErrAccountLocked) {
			t.Fatalf("login while locked = %v, want ErrAccountLocked", err)
		}

		// And it opens again by itself. A lockout nobody can clear is an
		// administrative task for every mistyped password.
		f.advance(2 * time.Minute)

		if _, rerr := svc.Login(context.Background(), credentials(testPassword)); rerr != nil {
			t.Errorf("login after the window: %v", rerr)
		}
	})
}

/*
An account with no password cannot be logged into with one.

This is an SSO-only user, or one provisioned before a password was set. The
danger is not that it succeeds -- it is that a comparison against an empty
stored hash could be made to succeed by an empty password, which is exactly the
account an attacker would look for.
*/
func TestAnAccountWithNoPasswordCannotLogIn(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, f *fixture) {
		user, err := f.repos.Users.Create(f.ctx, repo.CreateUser{
			Email: "sso-only@example.com", Name: "Grace", IsActive: true,
		})
		if err != nil {
			t.Fatalf("create user: %v", err)
		}

		for _, password := range []string{"", "anything", testPassword} {
			_, lerr := f.svc.Login(context.Background(), auth.Credentials{
				OrgID: f.org.ID, Email: user.Email, Password: password, IP: "127.0.0.1",
			})

			if !errors.Is(lerr, auth.ErrInvalidCredentials) {
				t.Errorf("login with %q = %v, want ErrInvalidCredentials", password, lerr)
			}
		}
	})
}

/*
A stored hash that cannot be parsed is an operational problem, not a user one.

It has to fail closed and it has to be visible: reported to the caller as
ordinary invalid credentials, because "your stored hash is corrupt" tells an
attacker they have found an account worth attacking, and logged loudly, because
nobody will find it otherwise.
*/
func TestACorruptStoredHashFailsClosedAndIsLogged(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, f *fixture) {
		if err := f.repos.Users.SetPassword(f.ctx, f.user.ID, "$argon2id$not$a$hash"); err != nil {
			t.Fatalf("set corrupt hash: %v", err)
		}

		_, err := f.login(t, testPassword)

		if !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("login against a corrupt hash = %v, want ErrInvalidCredentials", err)
		}

		if !strings.Contains(f.logs.String(), "stored password hash is unusable") {
			t.Error("a corrupt stored hash was not logged; nobody would ever find it")
		}

		// And the hash itself never reaches the log, corrupt or not.
		if strings.Contains(f.logs.String(), "$argon2id$not$a$hash") {
			t.Error("the stored hash was written to the log")
		}
	})
}

/*
Changing a password ends every session it had.

This is the point of the operation. A password change is usually a response to
suspicion, and one that leaves the other party's session alive has done nothing
except make the owner feel safer.
*/
func TestChangingAPasswordEndsTheOtherSessions(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, f *fixture) {
		first, err := f.login(t, testPassword)
		if err != nil {
			t.Fatalf("first login: %v", err)
		}

		second, err := f.login(t, testPassword)
		if err != nil {
			t.Fatalf("second login: %v", err)
		}

		const replacement = "an-entirely-different-password"

		if serr := f.svc.SetPassword(f.ctx, f.user.ID, replacement); serr != nil {
			t.Fatalf("set password: %v", serr)
		}

		for name, token := range map[string]string{
			"first":  first.Token,
			"second": second.Token,
		} {
			if _, aerr := f.svc.Authenticate(context.Background(), token); !errors.Is(
				aerr, auth.ErrSessionInvalid) {
				t.Errorf("the %s session survived the password change: %v", name, aerr)
			}
		}

		if _, lerr := f.login(t, testPassword); !errors.Is(lerr, auth.ErrInvalidCredentials) {
			t.Error("the old password still works")
		}

		if _, lerr := f.login(t, replacement); lerr != nil {
			t.Errorf("the new password does not work: %v", lerr)
		}
	})
}

// A password that fails the floor is refused before anything is written, so a
// rejected change cannot leave an account with no usable password.
func TestAnUnacceptablePasswordChangesNothing(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, f *fixture) {
		for name, password := range map[string]string{
			"too short": "short",
			"too long":  strings.Repeat("x", auth.MaxPasswordLength+1),
		} {
			t.Run(name, func(t *testing.T) {
				if err := f.svc.SetPassword(f.ctx, f.user.ID, password); err == nil {
					t.Fatal("an unacceptable password was accepted")
				}
			})
		}

		// The original still works, which is the part that matters: a rejected
		// change that had already written the hash would lock the owner out.
		if _, err := f.login(t, testPassword); err != nil {
			t.Errorf("the original password stopped working: %v", err)
		}
	})
}
