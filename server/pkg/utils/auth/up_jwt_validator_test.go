package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/spf13/viper"
)

const (
	testIssuer = "https://keycloak.example/auth/realms/test"
	testClient = "up-photos"
)

func newTestValidator(t *testing.T, key *rsa.PrivateKey, issuers, audiences []string) (*JWTValidator, error) {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("jwt.public-keys", []string{string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))})
	viper.Set("jwt.issuers", issuers)
	viper.Set("jwt.audiences", audiences)
	return NewJWTValidator()
}

func validClaims() jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"iss":                testIssuer,
		"aud":                "account",
		"azp":                testClient,
		"preferred_username": "alice",
		"iat":                now.Unix(),
		"exp":                now.Add(5 * time.Minute).Unix(),
	}
}

func sign(t *testing.T, key *rsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestUPJWTValidatorRequiresIssuersAndAudiences(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	if _, err := newTestValidator(t, key, nil, []string{testClient}); err == nil {
		t.Fatal("expected error without jwt.issuers")
	}
	if _, err := newTestValidator(t, key, []string{testIssuer}, []string{" "}); err == nil {
		t.Fatal("expected error without jwt.audiences")
	}
}

func TestUPJWTValidator(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	otherKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	v, err := newTestValidator(t, key, []string{testIssuer}, []string{testClient})
	if err != nil {
		t.Fatal(err)
	}

	with := func(k string, val interface{}) jwt.MapClaims {
		c := validClaims()
		if val == nil {
			delete(c, k)
		} else {
			c[k] = val
		}
		return c
	}

	cases := []struct {
		name  string
		token string
		ok    bool
	}{
		{"valid via azp", sign(t, key, validClaims()), true},
		{"no azp, aud is account only", sign(t, key, with("azp", nil)), false},
		{"aud lists the client", sign(t, key, func() jwt.MapClaims { c := with("azp", "other"); c["aud"] = []string{"account", testClient}; return c }()), true},
		{"wrong issuer", sign(t, key, with("iss", "https://evil.example/realms/x")), false},
		{"other client", sign(t, key, with("azp", "admin-cli")), false},
		{"missing exp", sign(t, key, with("exp", nil)), false},
		{"expired", sign(t, key, with("exp", time.Now().Add(-time.Hour).Unix())), false},
		{"unknown key", sign(t, otherKey, validClaims()), false},
		{"HS256", func() string {
			s, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, validClaims()).SignedString([]byte("secret"))
			return s
		}(), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claims, err := v.ValidateToken("Bearer " + tc.token)
			if tc.ok && (err != nil || claims.PreferredUsername != "alice") {
				t.Fatalf("expected valid token, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected token to be rejected")
			}
		})
	}
}
