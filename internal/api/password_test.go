package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"github.com/Mmd4LIFE/pivot/internal/api"
	"github.com/Mmd4LIFE/pivot/internal/store"
)

/*
Changing your own password, over HTTP.

The service tests own the rules. What is checked here is the part a browser
meets, and one decision that only exists at this layer: a wrong current
password is a field error, not a 401. A 401 would reach the frontend's global
session handling and sign somebody out for mistyping their own password into a
form — a logout triggered by the safety feature.
*/

const passwordPath = api.APIPrefix + "/auth/password"

// changePassword posts a change using the fixture's signed-in cookie jar.
func (f *authFixture) changePassword(t *testing.T, current, next string) response {
	t.Helper()

	return f.request(t, http.MethodPost, passwordPath, map[string]string{
		"currentPassword": current,
		"newPassword":     next,
	})
}

// secondDevice signs the same user in on a separate client with its own cookie
// jar, which is the only way to have two live sessions to tell apart.
func (f *authFixture) secondDevice(t *testing.T) *http.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}

	client := newTestClient(jar)

	body, err := json.Marshal(map[string]string{
		"email": fixtureEmail, "password": fixturePassword,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req, err := http.NewRequestWithContext(context.Background(),
		http.MethodPost, f.server.URL+api.APIPrefix+"/auth/login", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if resp := send(t, client, req); resp.status != http.StatusOK {
		t.Fatalf("second device login = %d: %s", resp.status, resp)
	}

	return client
}

// me asks who the given client is signed in as.
func (f *authFixture) me(t *testing.T, client *http.Client) response {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(),
		http.MethodGet, f.server.URL+api.APIPrefix+"/auth/me", http.NoBody)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	return send(t, client, req)
}

const newPassword = "an-entirely-different-password"

// The deliverable: the password changes, the other devices are signed out, and
// the one doing the changing is not.
func TestChangingYourPasswordKeepsYouSignedInAndSignsOutTheRest(t *testing.T) {
	t.Parallel()

	bothEngines(t, func(t *testing.T, db *store.DB) {
		f := newAuthFixture(t, db)

		if resp := f.loginPassword(t); resp.status != http.StatusOK {
			t.Fatalf("login: %d", resp.status)
		}

		// A second device: its own client, its own jar, its own session.
		other := f.secondDevice(t)

		resp := f.changePassword(t, fixturePassword, newPassword)
		if resp.status != http.StatusNoContent {
			t.Fatalf("change = %d, want 204: %s", resp.status, resp)
		}

		// This session still works. Signing somebody out of the device they
		// are standing at makes the safe action feel like a punishment.
		if me := f.me(t, f.client); me.status != http.StatusOK {
			t.Errorf("the session that changed the password was ended: %d", me.status)
		}

		// The other one does not. This is the entire point of the operation:
		// a change made in response to suspicion that leaves the other party
		// signed in has done nothing but make the owner feel safer.
		if me := f.me(t, other); me.status != http.StatusUnauthorized {
			t.Errorf("the other session survived the password change: %d", me.status)
		}
	})
}

// The new password works and the old one does not, which is the part somebody
// finds out about tomorrow morning.
func TestAfterAChangeOnlyTheNewPasswordWorks(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t, openSQLite(t))

	if resp := f.loginPassword(t); resp.status != http.StatusOK {
		t.Fatalf("login: %d", resp.status)
	}

	if resp := f.changePassword(t, fixturePassword, newPassword); resp.status != http.StatusNoContent {
		t.Fatalf("change = %d: %s", resp.status, resp)
	}

	if resp := f.login(t, fixtureEmail, fixturePassword); resp.status == http.StatusOK {
		t.Error("the old password still signs in")
	}

	if resp := f.login(t, fixtureEmail, newPassword); resp.status != http.StatusOK {
		t.Errorf("the new password does not sign in: %d", resp.status)
	}
}

/*
A wrong current password is a field error, not a 401.

The frontend turns every 401 into "your session ended, go and log in again".
Returning one here would mean mistyping your own password in the change form
logs you out -- a logout caused by the safety feature, which is how people
learn not to use it.
*/
func TestAWrongCurrentPasswordDoesNotSignYouOut(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t, openSQLite(t))

	if resp := f.loginPassword(t); resp.status != http.StatusOK {
		t.Fatalf("login: %d", resp.status)
	}

	resp := f.changePassword(t, "not-my-password", newPassword)

	if resp.status != http.StatusUnprocessableEntity {
		t.Fatalf("change = %d, want 422: %s", resp.status, resp)
	}

	if !strings.Contains(string(resp.body), `"field":"currentPassword"`) {
		t.Errorf("the error does not name the field: %s", resp.body)
	}

	// And the session is untouched, which is the assertion this test exists
	// for.
	if me := f.me(t, f.client); me.status != http.StatusOK {
		t.Errorf("a wrong current password ended the session: %d", me.status)
	}

	// The real password still works, so nothing was written on the way to
	// refusing.
	if again := f.changePassword(t, fixturePassword, newPassword); again.status != http.StatusNoContent {
		t.Errorf("the correct password was refused afterwards: %d", again.status)
	}
}

func TestAnUnacceptableNewPasswordIsRefused(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ current, next string }{
		"too short":       {fixturePassword, "short"},
		"empty":           {fixturePassword, ""},
		"no current":      {"", newPassword},
		"same as current": {fixturePassword, fixturePassword},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newAuthFixture(t, openSQLite(t))

			if resp := f.loginPassword(t); resp.status != http.StatusOK {
				t.Fatalf("login: %d", resp.status)
			}

			if resp := f.changePassword(t, tc.current, tc.next); resp.status != http.StatusUnprocessableEntity {
				t.Fatalf("change = %d, want 422: %s", resp.status, resp)
			}

			// Unchanged, so a refused change cannot leave an account whose
			// owner no longer knows the password.
			if resp := f.login(t, fixtureEmail, fixturePassword); resp.status != http.StatusOK {
				t.Errorf("the original password stopped working: %d", resp.status)
			}
		})
	}
}

// Nobody signed in, nothing to change. The route is behind the session
// middleware, which is what makes this a 401 rather than a handler deciding.
func TestChangingAPasswordRequiresASession(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t, openSQLite(t))

	if resp := f.changePassword(t, fixturePassword, newPassword); resp.status != http.StatusUnauthorized {
		t.Fatalf("change without a session = %d, want 401: %s", resp.status, resp)
	}
}
