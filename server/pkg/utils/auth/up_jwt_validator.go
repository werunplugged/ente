package auth

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ente/museum/ente"
	"github.com/ente/stacktrace"
	"github.com/golang-jwt/jwt/v5"
	"github.com/spf13/viper"
)

// JWTValidator is responsible for validating JWT tokens
type JWTValidator struct {
	publicKeys []*rsa.PublicKey
	issuers    []string
	audiences  []string
	parser     *jwt.Parser
}

// JWTClaims represents the claims in a JWT token
type JWTClaims struct {
	PreferredUsername string `json:"preferred_username,omitempty"`
	AuthorizedParty   string `json:"azp,omitempty"`
	jwt.RegisteredClaims
}

// NewJWTValidator creates a new JWTValidator instance
func NewJWTValidator() (*JWTValidator, error) {
	// Get the public key from the configuration
	publicKeyPEMs := viper.GetStringSlice("jwt.public-keys")
	if len(publicKeyPEMs) == 0 {
		// Fallback to single key for backward compatibility
		publicKeyPEM := viper.GetString("jwt.public-key")
		if publicKeyPEM == "" {
			return nil, stacktrace.Propagate(fmt.Errorf("neither jwt.public-keys nor jwt.public-key found in configuration"), "")
		}
		publicKeyPEMs = []string{publicKeyPEM}
	}

	// Parse all public keys
	var publicKeys []*rsa.PublicKey
	for i, publicKeyPEM := range publicKeyPEMs {
		publicKey, err := jwt.ParseRSAPublicKeyFromPEM([]byte(publicKeyPEM))
		if err != nil {
			return nil, stacktrace.Propagate(err, "failed to parse RSA public key at index %d", i)
		}
		publicKeys = append(publicKeys, publicKey)
	}

	// Without these, any token signed by a trusted key would be accepted,
	// whichever client it was issued for, so refuse to start.
	issuers := nonEmpty(viper.GetStringSlice("jwt.issuers"))
	if len(issuers) == 0 {
		return nil, stacktrace.Propagate(fmt.Errorf("jwt.issuers is not configured"), "")
	}
	audiences := nonEmpty(viper.GetStringSlice("jwt.audiences"))
	if len(audiences) == 0 {
		return nil, stacktrace.Propagate(fmt.Errorf("jwt.audiences is not configured"), "")
	}

	return &JWTValidator{
		publicKeys: publicKeys,
		issuers:    issuers,
		audiences:  audiences,
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{"RS256"}),
			jwt.WithExpirationRequired(),
			jwt.WithIssuedAt(),
			jwt.WithLeeway(30*time.Second),
		),
	}, nil
}

func nonEmpty(values []string) []string {
	var result []string
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			result = append(result, v)
		}
	}
	return result
}

// checkIssuerAndAudience requires a configured issuer, and a configured
// audience in either aud or azp (Keycloak access tokens name the client in
// azp and often carry only "account" in aud).
func (v *JWTValidator) checkIssuerAndAudience(claims *JWTClaims) error {
	if !slices.Contains(v.issuers, claims.Issuer) {
		return fmt.Errorf("unexpected issuer %q", claims.Issuer)
	}
	if slices.Contains(v.audiences, claims.AuthorizedParty) {
		return nil
	}
	for _, aud := range claims.Audience {
		if slices.Contains(v.audiences, aud) {
			return nil
		}
	}
	return fmt.Errorf("token not issued for an allowed audience")
}

// ValidateToken validates a JWT token and returns the claims
func (v *JWTValidator) ValidateToken(tokenString string) (*JWTClaims, error) {
	// Remove "Bearer " prefix if present
	tokenString = strings.TrimPrefix(tokenString, "Bearer ")

	var lastErr error
	// Try validation with each public key
	for _, publicKey := range v.publicKeys {
		token, err := v.parser.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
			// Validate the signing method
			if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, stacktrace.Propagate(fmt.Errorf("unexpected signing method: %v", token.Header["alg"]), "")
			}
			return publicKey, nil
		})

		if err != nil {
			lastErr = err
			continue // Try next key
		}

		// Check if the token is valid
		if !token.Valid {
			lastErr = fmt.Errorf("invalid token")
			continue // Try next key
		}

		// Extract the claims
		claims, ok := token.Claims.(*JWTClaims)
		if !ok {
			lastErr = fmt.Errorf("failed to extract claims")
			continue // Try next key
		}

		// The signature matched this key, so another key won't change the outcome
		if err := v.checkIssuerAndAudience(claims); err != nil {
			return nil, stacktrace.Propagate(err, "JWT validation failed")
		}

		// Successfully validated with key at index i
		return claims, nil
	}

	// If we get here, validation failed with all keys
	if errors.Is(lastErr, jwt.ErrTokenExpired) {
		return nil, stacktrace.Propagate(ente.NewBadRequestWithMessage("token expired"), "")
	}
	return nil, stacktrace.Propagate(lastErr, "JWT validation failed with all public keys")

}

// GetPreferredUsername extracts the preferred_username claim from a JWT token
func (v *JWTValidator) GetPreferredUsername(tokenString string) (string, error) {
	claims, err := v.ValidateToken(tokenString)
	if err != nil {
		return "", err
	}
	return claims.PreferredUsername, nil
}
