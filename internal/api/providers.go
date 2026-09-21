package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Mmd4LIFE/pivot/internal/oidc"
	"github.com/Mmd4LIFE/pivot/internal/store/dbtypes"
	"github.com/Mmd4LIFE/pivot/internal/store/model"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
)

// Administration of identity providers. Gated on manage_organization: whoever
// can point an organization at a different directory can decide who its users
// are, which is the same power as granting roles by another route.

// providerResponse is the administrative view of a provider.
//
// Note what is absent and cannot be added by accident: ClientSecret. This
// struct is hand-written rather than derived from the row precisely so that
// the secret has to be typed in to leak, and a test asserts it never appears.
// The API is write-only for it — there is no read path, for an administrator
// or anyone else.
type providerResponse struct {
	ID            string            `json:"id"`
	Slug          string            `json:"slug"`
	Name          string            `json:"name"`
	Kind          string            `json:"kind"`
	Issuer        string            `json:"issuer"`
	ClientID      string            `json:"clientId"`
	Scopes        []string          `json:"scopes"`
	IsEnabled     bool              `json:"isEnabled"`
	AutoProvision bool              `json:"autoProvision"`
	LinkByEmail   bool              `json:"linkByEmail"`
	DefaultRole   string            `json:"defaultRole"`
	ClaimMapping  oidc.ClaimMapping `json:"claimMapping"`
	Version       int64             `json:"version"`

	// HasClientSecret says whether one is configured, without saying what it
	// is. An administration screen needs to show "configured" versus "not",
	// and that is the whole of what it needs.
	HasClientSecret bool `json:"hasClientSecret"`
}

func newProviderResponse(row model.IdentityProvider) providerResponse {
	mapping, err := oidc.UnmarshalMapping(row.ClaimMapping)
	if err != nil {
		mapping = oidc.ClaimMapping{}
	}

	return providerResponse{
		ID:              row.ID.String(),
		Slug:            row.Slug,
		Name:            row.Name,
		Kind:            row.Kind,
		Issuer:          row.Issuer,
		ClientID:        row.ClientID,
		Scopes:          oidc.ParseScopes(row.Scopes),
		IsEnabled:       row.IsEnabled.Bool(),
		AutoProvision:   row.AutoProvision.Bool(),
		LinkByEmail:     row.LinkByEmail.Bool(),
		DefaultRole:     row.DefaultRole,
		ClaimMapping:    mapping,
		Version:         row.Version,
		HasClientSecret: row.ClientSecret != "",
	}
}

type providerAdminListResponse struct {
	Providers []providerResponse `json:"providers"`
}

// providerRequest is the create and update body.
type providerRequest struct {
	Slug          string            `json:"slug"`
	Name          string            `json:"name"`
	Issuer        string            `json:"issuer"`
	ClientID      string            `json:"clientId"`
	ClientSecret  string            `json:"clientSecret,omitempty"`
	Scopes        []string          `json:"scopes,omitempty"`
	IsEnabled     *bool             `json:"isEnabled,omitempty"`
	AutoProvision *bool             `json:"autoProvision,omitempty"`
	LinkByEmail   *bool             `json:"linkByEmail,omitempty"`
	DefaultRole   string            `json:"defaultRole,omitempty"`
	ClaimMapping  oidc.ClaimMapping `json:"claimMapping,omitempty"`

	// Version is required on update, for optimistic concurrency.
	Version int64 `json:"version,omitempty"`
}

// maxIssuerLength bounds the URL.
const maxIssuerLength = 512

func (b *providerRequest) Validate() []Detail {
	details := Collect(
		Required("slug", b.Slug),
		Required("name", b.Name),
		Required("issuer", b.Issuer),
		Required("clientId", b.ClientID),
		MaxLen("issuer", b.Issuer, maxIssuerLength),
		MaxLen("slug", b.Slug, maxSlugLength),
	)

	// The issuer is fetched over the network at login time, so a value that is
	// not an https URL is refused here rather than at the first login attempt.
	// http is allowed only for a loopback host, which is what a local Keycloak
	// or a test provider is.
	if b.Issuer != "" && !validIssuer(b.Issuer) {
		details = append(details, Detail{
			Field:   "issuer",
			Message: "must be an https URL, or http on localhost",
		})
	}

	if b.Slug != "" && !validSlug(b.Slug) {
		details = append(details, Detail{
			Field:   "slug",
			Message: "may contain only lowercase letters, digits and hyphens",
		})
	}

	if b.DefaultRole != "" && !isBuiltinRoleName(b.DefaultRole) {
		details = append(details, Detail{Field: "defaultRole", Message: "must be one of " + joinRoles()})
	}

	return details
}

// validIssuer checks the scheme and host.
func validIssuer(issuer string) bool {
	switch {
	case strings.HasPrefix(issuer, "https://"):
		return true

	case strings.HasPrefix(issuer, "http://"):
		rest := strings.TrimPrefix(issuer, "http://")
		host, _, _ := strings.Cut(rest, "/")
		host, _, _ = strings.Cut(host, ":")

		return host == "localhost" || host == "127.0.0.1" || host == "[::1]" || host == "::1"

	default:
		return false
	}
}

// validSlug checks the URL segment.
func validSlug(slug string) bool {
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}

	return true
}

// --- handlers --------------------------------------------------------------

// handleAdminList returns every configured provider.
func (h *OIDCHandler) handleAdminList(w http.ResponseWriter, r *http.Request) {
	providers, err := h.repos.IdentityProviders.List(r.Context())
	if err != nil {
		WriteError(w, r, err)

		return
	}

	out := make([]providerResponse, 0, len(providers))
	for _, p := range providers {
		out = append(out, newProviderResponse(p))
	}

	WriteJSON(r.Context(), w, http.StatusOK, providerAdminListResponse{Providers: out})
}

// handleAdminCreate configures a new provider.
func (h *OIDCHandler) handleAdminCreate(w http.ResponseWriter, r *http.Request) {
	var body providerRequest
	if err := Decode(w, r, &body); err != nil {
		WriteError(w, r, err)

		return
	}

	mapping, err := oidc.MarshalMapping(body.ClaimMapping)
	if err != nil {
		WriteError(w, r, err)

		return
	}

	provider, err := h.repos.IdentityProviders.Create(r.Context(), repo.CreateIdentityProvider{
		Slug:          body.Slug,
		Name:          body.Name,
		Issuer:        strings.TrimRight(body.Issuer, "/"),
		ClientID:      body.ClientID,
		ClientSecret:  body.ClientSecret,
		Scopes:        strings.Join(body.Scopes, " "),
		IsEnabled:     boolOr(body.IsEnabled, true),
		AutoProvision: boolOr(body.AutoProvision, true),
		LinkByEmail:   boolOr(body.LinkByEmail, false),
		DefaultRole:   body.DefaultRole,
		ClaimMapping:  dbtypes.JSON(mapping),
	})
	if err != nil {
		if errors.Is(err, repo.ErrDuplicate) {
			WriteError(w, r, ValidationError(Detail{
				Field: "slug", Message: "a provider with that slug already exists",
			}))

			return
		}

		WriteError(w, r, err)

		return
	}

	// A new issuer may have been cached under an old configuration.
	h.registry.Forget(provider.Issuer)

	WriteJSON(r.Context(), w, http.StatusCreated, newProviderResponse(provider))
}

// handleAdminUpdate modifies a provider.
//
// An omitted clientSecret keeps the stored one. That is what makes the field
// write-only usable: an administration screen reads the provider, edits the
// name, and writes it back without ever having seen the secret — and without
// blanking it.
func (h *OIDCHandler) handleAdminUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, r, ValidationError(Detail{Field: "id", Message: "must be a UUID"}))

		return
	}

	var body providerRequest
	if derr := Decode(w, r, &body); derr != nil {
		WriteError(w, r, derr)

		return
	}

	current, err := h.repos.IdentityProviders.Get(r.Context(), id)
	if err != nil {
		WriteError(w, r, err)

		return
	}

	secret := body.ClientSecret
	if secret == "" {
		secret = current.ClientSecret
	}

	mapping, err := oidc.MarshalMapping(body.ClaimMapping)
	if err != nil {
		WriteError(w, r, err)

		return
	}

	scopes := strings.Join(body.Scopes, " ")
	if scopes == "" {
		scopes = current.Scopes
	}

	role := body.DefaultRole
	if role == "" {
		role = current.DefaultRole
	}

	provider, err := h.repos.IdentityProviders.Update(r.Context(), repo.UpdateIdentityProvider{
		ID:            id,
		Slug:          body.Slug,
		Name:          body.Name,
		Issuer:        strings.TrimRight(body.Issuer, "/"),
		ClientID:      body.ClientID,
		ClientSecret:  secret,
		Scopes:        scopes,
		IsEnabled:     boolOr(body.IsEnabled, current.IsEnabled.Bool()),
		AutoProvision: boolOr(body.AutoProvision, current.AutoProvision.Bool()),
		LinkByEmail:   boolOr(body.LinkByEmail, current.LinkByEmail.Bool()),
		DefaultRole:   role,
		ClaimMapping:  dbtypes.JSON(mapping),
		Version:       body.Version,
	})
	if err != nil {
		WriteError(w, r, err)

		return
	}

	// Discovery is cached per issuer. An administrator who has just repointed
	// a provider should not have to wait out the TTL to see it take effect.
	h.registry.Forget(current.Issuer)
	h.registry.Forget(provider.Issuer)

	WriteJSON(r.Context(), w, http.StatusOK, newProviderResponse(provider))
}

// handleAdminDelete removes a provider.
func (h *OIDCHandler) handleAdminDelete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, r, ValidationError(Detail{Field: "id", Message: "must be a UUID"}))

		return
	}

	current, err := h.repos.IdentityProviders.Get(r.Context(), id)
	if err != nil {
		WriteError(w, r, err)

		return
	}

	if derr := h.repos.IdentityProviders.SoftDelete(r.Context(), id); derr != nil {
		WriteError(w, r, derr)

		return
	}

	h.registry.Forget(current.Issuer)

	w.WriteHeader(http.StatusNoContent)
}

// boolOr resolves an optional boolean against a default.
//
// The request fields are pointers so that "absent" and "false" are
// distinguishable. Without that, a partial update would silently switch off
// every flag it did not mention.
func boolOr(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}

	return *v
}
