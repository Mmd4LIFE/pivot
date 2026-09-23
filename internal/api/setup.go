package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Mmd4LIFE/pivot/internal/auth"
	"github.com/Mmd4LIFE/pivot/internal/logging"
	"github.com/Mmd4LIFE/pivot/internal/setup"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

// SetupHandler serves the first run.
//
// It holds the auth handler rather than its own session logic on purpose:
// claiming an instance ends with being logged in, and the way to be logged in
// is the login endpoint's way. A second path to a session cookie is a second
// path to get the cookie's flags wrong.
type SetupHandler struct {
	svc  *setup.Service
	auth *AuthHandler
	log  *slog.Logger
}

// NewSetupHandler builds the first-run endpoints.
func NewSetupHandler(svc *setup.Service, authHandler *AuthHandler, log *slog.Logger) *SetupHandler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &SetupHandler{svc: svc, auth: authHandler, log: log}
}

// setupStatusResponse is what the browser asks before deciding which page to
// show.
type setupStatusResponse struct {
	// Initialized is true once this Pivot has an organization.
	Initialized bool `json:"initialized"`

	// TokenRequired tells the setup form whether to ask for the token.
	//
	// Safe to publish: an unclaimed instance will say so to whoever asks
	// either way, and a form that hides the field when a token is needed just
	// produces a rejection the person cannot act on.
	TokenRequired bool `json:"tokenRequired"`
}

// handleStatus reports whether this Pivot has been claimed.
func (h *SetupHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.svc.Status(r.Context())
	if err != nil {
		WriteError(w, r, NewError(CodeUnavailable,
			"The instance status could not be read", err))

		return
	}

	WriteJSON(r.Context(), w, http.StatusOK, setupStatusResponse{
		Initialized:   status.Initialized,
		TokenRequired: h.svc.TokenRequired(),
	})
}

// setupRequest claims an instance.
type setupRequest struct {
	Organization string `json:"organization"`
	Name         string `json:"name"`
	Email        string `json:"email"`
	Password     string `json:"password"`
	Token        string `json:"token"`
}

// Validate checks the shape of the request, not the policy.
//
// The password floor is [auth.MinPasswordLength] and is enforced by the
// hasher, which is the only place that can enforce it for every caller. What
// is checked here is what the hasher cannot see: that the fields exist at all.
func (b setupRequest) Validate() []Detail {
	var details []Detail

	if strings.TrimSpace(b.Organization) == "" {
		details = append(details, Detail{
			Field: "organization", Message: "An organization name is required",
		})
	}

	if strings.TrimSpace(b.Email) == "" {
		details = append(details, Detail{Field: "email", Message: "An email address is required"})
	}

	if b.Password == "" {
		details = append(details, Detail{Field: "password", Message: "A password is required"})
	}

	return details
}

// handleInitialize creates the first organization and administrator, and signs
// them in.
//
// Signing in is part of the deliverable rather than a convenience. The promise
// is download, run, open a browser, and be using Pivot; an account that then
// has to be typed into a login form is a fourth step, and the password was
// just typed twice.
func (h *SetupHandler) handleInitialize(w http.ResponseWriter, r *http.Request) {
	var body setupRequest
	if err := Decode(w, r, &body); err != nil {
		WriteError(w, r, err)

		return
	}

	ctx := r.Context()

	result, err := h.svc.Initialize(ctx, setup.Request{
		Organization: body.Organization,
		Name:         body.Name,
		Email:        body.Email,
		Password:     body.Password,
		Token:        body.Token,
	})
	if err != nil {
		WriteError(w, r, setupError(err))

		return
	}

	// Logged at Warn, not Info. An instance being claimed happens once in its
	// life and is the single most security-relevant event in its log: if it
	// appears twice, or at a time nobody recognizes, somebody needs to know.
	logging.FromContext(ctx).WarnContext(ctx, "this Pivot was claimed",
		slog.String("organization", result.OrgSlug),
		slog.String("email", result.Email),
		slog.String("user_id", result.UserID.String()),
		slog.String("remote", clientIP(r)))

	// The login path, not a copy of it.
	login, err := h.auth.svc.Login(ctx, auth.Credentials{
		OrgID:     result.OrgID,
		Email:     result.Email,
		Password:  body.Password,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	})
	if err != nil {
		// The account exists; only the session did not. Saying so is better
		// than a bare 500, because the person can simply log in -- and a retry
		// of setup would now correctly refuse.
		WriteError(w, r, NewError(CodeUnavailable,
			"The account was created but could not be signed in; please log in", err))

		return
	}

	h.auth.cookie.set(w, r, login.Token, login.Session.AbsoluteExpiresAt.Time)

	WriteJSON(ctx, w, http.StatusCreated, sessionEnvelope{
		User:    newUserResponse(login.User),
		Session: newSessionResponse(login.Session, true),
	})
}

// setupError maps the service's failures onto the error contract.
func setupError(err error) error {
	switch {
	case errors.Is(err, setup.ErrAlreadyInitialized):
		return NewError(CodeAlreadyInitialized, CodeAlreadyInitialized.Summary(), err)

	case errors.Is(err, setup.ErrTokenRequired), errors.Is(err, setup.ErrTokenInvalid):
		// One code for both. Whether a token was absent or wrong is not the
		// caller's business to learn from an unauthenticated endpoint, and the
		// person doing a first run has the same next step either way: read the
		// token out of the server's log.
		return NewError(CodeSetupTokenInvalid, CodeSetupTokenInvalid.Summary(), err)

	case errors.Is(err, auth.ErrPasswordTooShort), errors.Is(err, auth.ErrPasswordTooLong):
		return ValidationError(Detail{Field: "password", Message: err.Error()})

	case errors.Is(err, repo.ErrDuplicate):
		return NewError(CodeDuplicate, "That organization or email is already taken", err)

	default:
		return err
	}
}
