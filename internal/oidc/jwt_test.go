package oidc_test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// Minimal RS256 signing, so the fake provider issues tokens that are real
// enough for go-oidc to verify or reject on their merits.
//
// Hand-written rather than pulled from a library because it is twenty lines
// and because the test dependency would then be the same code under test in
// another guise. Nothing here is used outside tests.

func signRS256(key *rsa.PrivateKey, keyID string, claims map[string]any) (string, error) {
	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": keyID}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("encode header: %w", err)
	}

	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("encode claims: %w", err)
	}

	signingInput := b64(headerJSON) + "." + b64(claimsJSON)

	digest := sha256.Sum256([]byte(signingInput))

	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}

	return signingInput + "." + b64(signature), nil
}

func b64(in []byte) string { return base64.RawURLEncoding.EncodeToString(in) }

// s256 is the PKCE code challenge derivation.
func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))

	return base64.RawURLEncoding.EncodeToString(sum[:])
}
