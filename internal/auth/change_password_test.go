package auth_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/auth"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

/*
Changing your own password, as opposed to having it reset for you.

The difference is what is proved. An administrative reset proves nothing about
the person asking and therefore ends every session, including the one making
the request. A change proves the current password and therefore keeps the
session that proved it -- and ends all the others, which is the reason somebody
changes a password in the first place.
*/

const replacementPassword = "an-entirely-different-password"

// The deliverable: the other devices are signed out and this one is not.
func TestChangePasswordKeepsThisSessionAndEndsTheOthers(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, f *fixture) {
		here, err := f.login(t, testPassword)
		if err != nil {
			t.Fatalf("login here: %v", err)
		}

		elsewhere, err := f.login(t, testPassword)
		if err != nil {
			t.Fatalf("login elsewhere: %v", err)
		}

		if cerr := f.svc.ChangePassword(f.ctx,
			f.user.ID, here.Session.ID, testPassword, replacementPassword); cerr != nil {
			t.Fatalf("change password: %v", cerr)
		}

		if _, aerr := f.svc.Authenticate(context.Background(), here.Token); aerr != nil {
			t.Errorf("the session that changed the password was ended: %v", aerr)
		}

		if _, aerr := f.svc.Authenticate(context.Background(), elsewhere.Token); !errors.Is(
			aerr, auth.ErrSessionInvalid) {
			t.Errorf("the other session survived: %v", aerr)
		}

		if _, lerr := f.login(t, replacementPassword); lerr != nil {
			t.Errorf("the new password does not work: %v", lerr)
		}

		if _, lerr := f.login(t, testPassword); !errors.Is(lerr, auth.ErrInvalidCredentials) {
			t.Error("the old password still works")
		}
	})
}

/*
The current password is proved first.

Without it, an unlocked laptop or an XSS bug is enough to take the account
outright: a session cookie says somebody logged in at some point, not that the
person at the keyboard now is the same one.
*/
func TestChangePasswordRefusesAWrongCurrentPassword(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, f *fixture) {
		here, err := f.login(t, testPassword)
		if err != nil {
			t.Fatalf("login: %v", err)
		}

		cerr := f.svc.ChangePassword(f.ctx,
			f.user.ID, here.Session.ID, "not-the-current-password", replacementPassword)

		if !errors.Is(cerr, auth.ErrPasswordIncorrect) {
			t.Fatalf("error = %v, want ErrPasswordIncorrect", cerr)
		}

		// Nothing moved: not the password, and not the sessions. A refusal
		// that had already revoked would sign somebody out for a typo.
		if _, lerr := f.login(t, testPassword); lerr != nil {
			t.Errorf("the original password stopped working: %v", lerr)
		}

		if _, aerr := f.svc.Authenticate(context.Background(), here.Token); aerr != nil {
			t.Errorf("a refused change ended the session: %v", aerr)
		}
	})
}

/*
An account with no password cannot acquire one this way.

Setting the first password on an SSO-only account is a different operation with
a different proof. Accepting an empty current password here would be that
operation by accident, and it would let anybody holding an SSO session convert
it into a password they chose.
*/
func TestChangePasswordRefusesAnAccountWithNoPassword(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, f *fixture) {
		user, err := f.repos.Users.Create(f.ctx, repo.CreateUser{
			Email: "sso-only@example.com", Name: "Grace", IsActive: true,
		})
		if err != nil {
			t.Fatalf("create user: %v", err)
		}

		for _, current := range []string{"", "anything"} {
			cerr := f.svc.ChangePassword(f.ctx, user.ID, f.user.ID, current, replacementPassword)

			if !errors.Is(cerr, auth.ErrPasswordIncorrect) {
				t.Errorf("with current %q: error = %v, want ErrPasswordIncorrect", current, cerr)
			}
		}
	})
}

// A new password that fails the floor changes nothing, because the hash is
// computed before anything is written.
func TestChangePasswordRefusesAnUnacceptableNewPassword(t *testing.T) {
	t.Parallel()

	eachEngine(t, func(t *testing.T, f *fixture) {
		here, err := f.login(t, testPassword)
		if err != nil {
			t.Fatalf("login: %v", err)
		}

		for name, next := range map[string]string{
			"too short": "short",
			"too long":  strings.Repeat("x", auth.MaxPasswordLength+1),
		} {
			if cerr := f.svc.ChangePassword(f.ctx,
				f.user.ID, here.Session.ID, testPassword, next); cerr == nil {
				t.Errorf("%s: an unacceptable password was accepted", name)
			}
		}

		if _, lerr := f.login(t, testPassword); lerr != nil {
			t.Errorf("the original password stopped working: %v", lerr)
		}
	})
}
