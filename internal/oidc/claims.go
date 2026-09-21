package oidc

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Identity is a verified external identity, reduced to what Pivot stores.
//
// Everything here came out of a signature-checked ID token. Nothing in it is
// authoritative about *permissions* — that is the authorization layer's
// business — but Groups and Attributes feed the inputs those decisions are
// made from, which is why they are mapped explicitly rather than copied
// wholesale.
type Identity struct {
	// Subject is the provider's stable identifier for this person. It is what
	// a returning user is matched on, and never the email address.
	Subject string

	Email string

	// EmailVerified is the provider's assertion that it owns the address.
	// It gates account linking, which is the one place an address is allowed
	// to influence *which* account a login resolves to.
	EmailVerified bool

	Name string

	// Groups are the provider's group names, mapped onto Pivot groups by name.
	Groups []string

	// Attributes feed user_attributes, which Phase 4's row-level security
	// reads. They are recorded with source 'oidc' so a later sync can replace
	// exactly what it owns without disturbing anything set by hand.
	Attributes map[string]string
}

// ClaimMapping says which claim means what.
//
// Identity providers disagree about names — Okta says `groups`, Entra says
// `roles`, a self-hosted Keycloak says whatever its administrator configured —
// so this is configuration rather than a constant. The zero value uses the
// standard OpenID Connect claim names, which is right for most providers.
type ClaimMapping struct {
	// Email names the claim holding the address. Default "email".
	Email string `json:"email,omitempty"`

	// Name names the claim holding the display name. Default "name".
	Name string `json:"name,omitempty"`

	// EmailVerified names the claim asserting the address is verified.
	// Default "email_verified".
	EmailVerified string `json:"emailVerified,omitempty"`

	// Groups names the claim holding group membership. Default "groups".
	Groups string `json:"groups,omitempty"`

	// Attributes maps a claim name to the user-attribute key it becomes.
	// Only the claims listed here are copied: a provider's token routinely
	// carries things Pivot has no business storing.
	Attributes map[string]string `json:"attributes,omitempty"`
}

// Default claim names, per the OpenID Connect core specification.
const (
	DefaultEmailClaim  = "email"
	DefaultNameClaim   = "name"
	DefaultGroupsClaim = "groups"

	// DefaultEmailVerifiedClaim is standard in OpenID Connect core.
	DefaultEmailVerifiedClaim = "email_verified"
)

// withDefaults fills in the standard claim names.
func (m ClaimMapping) withDefaults() ClaimMapping {
	if m.Email == "" {
		m.Email = DefaultEmailClaim
	}

	if m.Name == "" {
		m.Name = DefaultNameClaim
	}

	if m.Groups == "" {
		m.Groups = DefaultGroupsClaim
	}

	if m.EmailVerified == "" {
		m.EmailVerified = DefaultEmailVerifiedClaim
	}

	return m
}

// Apply reduces a claim set to an [Identity].
func (m ClaimMapping) Apply(subject string, claims map[string]any) Identity {
	mapping := m.withDefaults()

	id := Identity{
		Subject:       subject,
		Email:         strings.TrimSpace(stringClaim(claims, mapping.Email)),
		EmailVerified: boolClaim(claims, mapping.EmailVerified),
		Name:          strings.TrimSpace(stringClaim(claims, mapping.Name)),
		Groups:        stringSliceClaim(claims, mapping.Groups),
		Attributes:    make(map[string]string, len(mapping.Attributes)),
	}

	for claim, key := range mapping.Attributes {
		if value := stringClaim(claims, claim); value != "" {
			id.Attributes[key] = value
		}
	}

	return id
}

// stringClaim reads a claim as a string.
//
// Numbers and booleans are rendered rather than dropped: a provider that sends
// `"employee_id": 4711` means something by it, and refusing to map it because
// JSON typed it as a number would be pedantry.
func stringClaim(claims map[string]any, name string) string {
	if name == "" {
		return ""
	}

	switch v := claims[name].(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}

		return "false"
	case float64:
		// json.Number-free rendering: %v on a float64 gives 4711 for whole
		// numbers rather than 4711.000000.
		return fmt.Sprintf("%v", v)
	case json.Number:
		return v.String()
	default:
		return ""
	}
}

// boolClaim reads a claim as a boolean.
//
// Providers send email_verified as a JSON boolean, and some send the string
// "true". Anything else - including the claim being absent - is false, which
// is the safe reading for a value that gates account linking.
func boolClaim(claims map[string]any, name string) bool {
	if name == "" {
		return false
	}

	switch v := claims[name].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true")
	default:
		return false
	}
}

// stringSliceClaim reads a claim as a list of strings.
//
// Providers send group membership in at least three shapes: a JSON array, a
// single string when there is exactly one group, and a space- or
// comma-separated string. Accepting all of them here is far less painful than
// discovering the difference in production.
func stringSliceClaim(claims map[string]any, name string) []string {
	if name == "" {
		return nil
	}

	var out []string

	switch v := claims[name].(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}

	case []string:
		for _, s := range v {
			if strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}

	case string:
		for _, s := range strings.FieldsFunc(v, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n'
		}) {
			if strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	}

	// Sorted and deduplicated, so a provider that reorders its groups between
	// logins does not look like a membership change to the sync below.
	sort.Strings(out)

	return dedupe(out)
}

func dedupe(in []string) []string {
	if len(in) == 0 {
		return nil
	}

	out := in[:1]

	for _, s := range in[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}

	return out
}

// MarshalMapping renders a mapping for storage.
func MarshalMapping(m ClaimMapping) ([]byte, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("oidc: encode claim mapping: %w", err)
	}

	return data, nil
}

// UnmarshalMapping reads a stored mapping.
//
// An empty or absent document is the default mapping, not an error: a provider
// configured without one uses the standard claim names.
func UnmarshalMapping(data []byte) (ClaimMapping, error) {
	if len(data) == 0 {
		return ClaimMapping{}, nil
	}

	var m ClaimMapping
	if err := json.Unmarshal(data, &m); err != nil {
		return ClaimMapping{}, fmt.Errorf("oidc: decode claim mapping: %w", err)
	}

	return m, nil
}
