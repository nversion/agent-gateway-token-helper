package idtoken

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func makeToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"RS256"}`)) + "." + enc(payload) + ".sig"
}

func TestValidate(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	base := func() map[string]any {
		return map[string]any{
			"iss":   "https://issuer.example",
			"sub":   "alice",
			"aud":   "client",
			"exp":   now.Add(time.Hour).Unix(),
			"iat":   now.Unix(),
			"nonce": "n1",
		}
	}
	expect := Expect{Issuer: "https://issuer.example", ClientID: "client", Nonce: "n1", Now: now}

	tests := []struct {
		name    string
		mutate  func(map[string]any)
		expect  func(*Expect)
		wantErr string
	}{
		{name: "valid"},
		{name: "array audience", mutate: func(c map[string]any) { c["aud"] = []string{"other", "client"} }},
		{name: "wrong audience", mutate: func(c map[string]any) { c["aud"] = "other" }, wantErr: "audience"},
		{name: "wrong azp", mutate: func(c map[string]any) { c["azp"] = "other" }, wantErr: "azp"},
		{name: "wrong issuer", mutate: func(c map[string]any) { c["iss"] = "https://evil.example" }, wantErr: "issuer"},
		{name: "expired", mutate: func(c map[string]any) { c["exp"] = now.Add(-time.Hour).Unix() }, wantErr: "expired"},
		{name: "expired within leeway", mutate: func(c map[string]any) { c["exp"] = now.Add(-time.Minute).Unix() }},
		{name: "missing exp", mutate: func(c map[string]any) { delete(c, "exp") }, wantErr: "exp"},
		{name: "issued in future", mutate: func(c map[string]any) { c["iat"] = now.Add(time.Hour).Unix() }, wantErr: "future"},
		{name: "wrong nonce", mutate: func(c map[string]any) { c["nonce"] = "other" }, wantErr: "nonce"},
		{name: "refresh without nonce", mutate: func(c map[string]any) { delete(c, "nonce") }, expect: func(e *Expect) {
			e.Nonce = ""
			e.Subject = "alice"
		}},
		{name: "refresh changes subject", expect: func(e *Expect) {
			e.Nonce = ""
			e.Subject = "bob"
		}, wantErr: "subject"},
		{name: "entra tenant template", mutate: func(c map[string]any) {
			c["iss"] = "https://login.microsoftonline.com/tid-1/v2.0"
			c["tid"] = "tid-1"
		}, expect: func(e *Expect) { e.Issuer = "https://login.microsoftonline.com/{tenantid}/v2.0" }},
		{name: "entra tenant mismatch", mutate: func(c map[string]any) {
			c["iss"] = "https://login.microsoftonline.com/tid-2/v2.0"
			c["tid"] = "tid-1"
		}, expect: func(e *Expect) { e.Issuer = "https://login.microsoftonline.com/{tenantid}/v2.0" }, wantErr: "issuer"},
		{name: "entra refresh changes tenant", mutate: func(c map[string]any) {
			c["iss"] = "https://login.microsoftonline.com/tid-2/v2.0"
			c["tid"] = "tid-2"
		}, expect: func(e *Expect) {
			e.Issuer = "https://login.microsoftonline.com/{tenantid}/v2.0"
			e.Nonce = ""
			e.Subject = "alice"
			e.SignedInIssuer = "https://login.microsoftonline.com/tid-1/v2.0"
		}, wantErr: "issuer"},
		{name: "entra template without tid", expect: func(e *Expect) {
			e.Issuer = "https://login.microsoftonline.com/{tenantid}/v2.0"
		}, wantErr: "tid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := base()
			if tt.mutate != nil {
				tt.mutate(claims)
			}
			e := expect
			if tt.expect != nil {
				tt.expect(&e)
			}
			c, err := Parse(makeToken(t, claims))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			err = c.Validate(e)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("Validate: unexpected error %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("Validate: error %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseMalformed(t *testing.T) {
	for _, raw := range []string{"", "a.b", "a.!!!.c", "a." + base64.RawURLEncoding.EncodeToString([]byte("not json")) + ".c"} {
		if _, err := Parse(raw); err == nil {
			t.Errorf("Parse(%q): expected error", raw)
		}
	}
}

func TestNumericDateFractional(t *testing.T) {
	got := NumericDate(1.5).Time()
	if want := time.Unix(1, 5e8); !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDecode(t *testing.T) {
	header, payload, err := Decode(makeToken(t, map[string]any{"sub": "alice"}))
	if err != nil {
		t.Fatal(err)
	}
	if string(header) != `{"alg":"RS256"}` || string(payload) != `{"sub":"alice"}` {
		t.Fatalf("header %s payload %s", header, payload)
	}
	bad := base64.RawURLEncoding.EncodeToString([]byte("not json"))
	if _, _, err := Decode(bad + ".e30.sig"); err == nil || !strings.Contains(err.Error(), "header") {
		t.Fatalf("expected header error, got %v", err)
	}
}
