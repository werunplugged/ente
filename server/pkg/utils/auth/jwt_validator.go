package auth

import (
	"crypto/rsa"
	"fmt"
	"strings"

	"github.com/ente-io/museum/ente"
	"github.com/ente-io/stacktrace"
	"github.com/golang-jwt/jwt"
	"github.com/spf13/viper"
)

// JWTValidator is responsible for validating JWT tokens
type JWTValidator struct {
	publicKey *rsa.PublicKey
}

// JWTClaims represents the claims in a JWT token
type JWTClaims struct {
	PreferredUsername string `json:"preferred_username,omitempty"`
	jwt.StandardClaims
}

// NewJWTValidator creates a new JWTValidator instance
func NewJWTValidator() (*JWTValidator, error) {
	// Get the public key from the configuration
	publicKeyPEM := viper.GetString("jwt.public-key")
	if publicKeyPEM == "" {
		return nil, stacktrace.Propagate(fmt.Errorf("jwt.public-key not found in configuration"), "")
	}

	// Parse the public key
	publicKey, err := jwt.ParseRSAPublicKeyFromPEM([]byte(publicKeyPEM))
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to parse RSA public key")
	}

	return &JWTValidator{
		publicKey: publicKey,
	}, nil
}

// ValidateToken validates a JWT token and returns the claims
func (v *JWTValidator) ValidateToken(tokenString string) (*JWTClaims, error) {
	// Remove "Bearer " prefix if present
	tokenString = strings.TrimPrefix(tokenString, "Bearer ")

	// Parse the token
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate the signing method
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, stacktrace.Propagate(fmt.Errorf("unexpected signing method: %v", token.Header["alg"]), "")
		}
		return v.publicKey, nil
	})

	if err != nil {
		if ve, ok := err.(*jwt.ValidationError); ok && ve.Error() == "token expired" {
			return nil, stacktrace.Propagate(ente.NewBadRequestWithMessage("token expired"), "")
		}
		return nil, stacktrace.Propagate(err, "JWT parsed failed")
	}

	// Check if the token is valid
	if !token.Valid {
		return nil, stacktrace.Propagate(fmt.Errorf("invalid token"), "")
	}

	// Extract the claims
	claims, ok := token.Claims.(*JWTClaims)
	if !ok {
		return nil, stacktrace.Propagate(fmt.Errorf("failed to extract claims"), "")
	}

	return claims, nil
}

// GetPreferredUsername extracts the preferred_username claim from a JWT token
func (v *JWTValidator) GetPreferredUsername(tokenString string) (string, error) {
	claims, err := v.ValidateToken(tokenString)
	if err != nil {
		return "", err
	}
	return claims.PreferredUsername, nil
}
