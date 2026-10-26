package auth

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"strings"

	"github.com/ente/museum/ente"
	"github.com/ente/stacktrace"
	"github.com/golang-jwt/jwt/v5"
	"github.com/spf13/viper"
)

// JWTValidator is responsible for validating JWT tokens
type JWTValidator struct {
	publicKeys []*rsa.PublicKey
}

// JWTClaims represents the claims in a JWT token
type JWTClaims struct {
	PreferredUsername string `json:"preferred_username,omitempty"`
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

	return &JWTValidator{
		publicKeys: publicKeys,
	}, nil
}

// ValidateToken validates a JWT token and returns the claims
func (v *JWTValidator) ValidateToken(tokenString string) (*JWTClaims, error) {
	// Remove "Bearer " prefix if present
	tokenString = strings.TrimPrefix(tokenString, "Bearer ")

	var lastErr error
	// Try validation with each public key
	for _, publicKey := range v.publicKeys {
		token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
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
