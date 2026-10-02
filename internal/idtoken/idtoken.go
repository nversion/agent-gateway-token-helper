// Package idtoken parses and validates the claims of an OIDC ID token.
//
// Signatures are deliberately not verified. OIDC Core 1.0 §3.1.3.7 (6) allows
// a client to rely on TLS server authentication instead of checking the
// signature when the ID token was received directly from the token endpoint,
// which is the only way this helper ever obtains one. The relying party that
// the token is presented to (the agent gateway) verifies the signature against
// the provider's JWKS.
package idtoken

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// TenantIDPlaceholder is the template Microsoft Entra ID uses in the issuer of
// its multi-tenant ("organizations", "common") discovery documents. It is
// replaced with the token's "tid" claim before the issuer is compared.
const TenantIDPlaceholder = "{tenantid}"

// DefaultLeeway is the clock skew tolerated when checking exp and iat.
const DefaultLeeway = 2 * time.Minute

// Claims holds the ID token claims this helper cares about.
type Claims struct {
	Issuer          string      `json:"iss"`
	Subject         string      `json:"sub"`
	Audience        Audience    `json:"aud"`
	AuthorizedParty string      `json:"azp,omitempty"`
	Expiry          NumericDate `json:"exp"`
	IssuedAt        NumericDate `json:"iat"`
	Nonce           string      `json:"nonce,omitempty"`
	Email           string      `json:"email,omitempty"`
	TenantID        string      `json:"tid,omitempty"`
}

// Audience is the "aud" claim, which is either a single string or an array.
type Audience []string

// UnmarshalJSON accepts both the string and the array form.
func (a *Audience) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*a = Audience{s}
		return nil
	}
	var l []string
	if err := json.Unmarshal(b, &l); err != nil {
		return fmt.Errorf("aud must be a string or an array of strings: %w", err)
	}
	*a = l
	return nil
}

// NumericDate is a JWT NumericDate: seconds since the epoch, possibly
// fractional.
type NumericDate float64

// Time converts the date to a time.Time; the zero date maps to the zero time.
func (d NumericDate) Time() time.Time {
	if d == 0 {
		return time.Time{}
	}
	sec := int64(d)
	return time.Unix(sec, int64((float64(d)-float64(sec))*1e9))
}

// Decode returns the JSON header and payload of a compact-serialized JWT
// without verifying its signature (see the package documentation).
func Decode(raw string) (header, payload json.RawMessage, err error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return nil, nil, fmt.Errorf("malformed ID token: expected 3 parts, got %d", len(parts))
	}
	for i, dst := range []*json.RawMessage{&header, &payload} {
		name := [...]string{"header", "payload"}[i]
		b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[i], "="))
		if err != nil {
			return nil, nil, fmt.Errorf("malformed ID token %s: %w", name, err)
		}
		if !json.Valid(b) {
			return nil, nil, fmt.Errorf("malformed ID token %s: not JSON", name)
		}
		*dst = b
	}
	return header, payload, nil
}

// Parse decodes the claims of a compact-serialized JWT without verifying its
// signature (see the package documentation).
func Parse(raw string) (*Claims, error) {
	_, payload, err := Decode(raw)
	if err != nil {
		return nil, err
	}
	var c Claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, fmt.Errorf("malformed ID token claims: %w", err)
	}
	return &c, nil
}

// Expect describes what a token has to look like to be accepted.
type Expect struct {
	// Issuer is the issuer from the provider's discovery document. It may
	// contain TenantIDPlaceholder.
	Issuer   string
	ClientID string
	// Nonce, when set, must equal the nonce claim. It is set for tokens from
	// an authorization code exchange and left empty for refreshed tokens,
	// which usually carry no nonce (OIDC Core §12.2).
	Nonce string
	// Subject, when set, must equal the sub claim. It is set on refresh so a
	// refreshed token can't switch identity.
	Subject string
	Now     time.Time
	Leeway  time.Duration
}

// Validate checks the claims against e.
func (c *Claims) Validate(e Expect) error {
	issuer := e.Issuer
	if strings.Contains(issuer, TenantIDPlaceholder) {
		if c.TenantID == "" {
			return errors.New("ID token has no tid claim to resolve the issuer template " + issuer)
		}
		issuer = strings.ReplaceAll(issuer, TenantIDPlaceholder, c.TenantID)
	}
	if c.Issuer != issuer {
		return fmt.Errorf("ID token issuer %q does not match expected %q", c.Issuer, issuer)
	}
	if c.Subject == "" {
		return errors.New("ID token has no sub claim")
	}
	if !slices.Contains(c.Audience, e.ClientID) {
		return fmt.Errorf("ID token audience %q does not contain client ID %q", c.Audience, e.ClientID)
	}
	if c.AuthorizedParty != "" && c.AuthorizedParty != e.ClientID {
		return fmt.Errorf("ID token azp %q does not match client ID %q", c.AuthorizedParty, e.ClientID)
	}

	now, leeway := e.Now, e.Leeway
	if now.IsZero() {
		now = time.Now()
	}
	if leeway == 0 {
		leeway = DefaultLeeway
	}
	if c.Expiry == 0 {
		return errors.New("ID token has no exp claim")
	}
	if exp := c.Expiry.Time(); !now.Before(exp.Add(leeway)) {
		return fmt.Errorf("ID token expired at %s", exp.Format(time.RFC3339))
	}
	if iat := c.IssuedAt.Time(); !iat.IsZero() && iat.After(now.Add(leeway)) {
		return fmt.Errorf("ID token issued in the future (%s)", iat.Format(time.RFC3339))
	}

	if e.Nonce != "" && c.Nonce != e.Nonce {
		return errors.New("ID token nonce does not match the authorization request")
	}
	if e.Subject != "" && c.Subject != e.Subject {
		return fmt.Errorf("refreshed ID token subject %q does not match %q", c.Subject, e.Subject)
	}
	return nil
}
