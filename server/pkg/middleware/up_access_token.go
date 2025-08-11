package middleware

import (
	"net/http"

	"github.com/ente-io/museum/ente"
	"github.com/ente-io/museum/pkg/utils/auth"
	"github.com/ente-io/museum/pkg/utils/handler"
	"github.com/ente-io/stacktrace"
	"github.com/gin-gonic/gin"
	"github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
)

const (
	// PreferredUsernameKey is the key used to store the preferred username in the context
	PreferredUsernameKey = "preferred_username"
)

// UPAccessTokenMiddleware intercepts and authenticates incoming requests using JWT tokens
type UPAccessTokenMiddleware struct {
	JWTValidator *auth.JWTValidator
	Cache        *cache.Cache
}

// UPAccessTokenAuthMiddleware returns a middleware that extracts the token from the Authorization header
// and uses it to authenticate the request. If the token is valid, it sets the user's preferred username
// in the context and allows the request to proceed. If the token is invalid, it aborts the request with
// an appropriate error response.
func (m *UPAccessTokenMiddleware) UPAccessTokenAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract token from Authorization header
		authHeader := c.GetHeader("Authorization")
		var request ente.SendOTTRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			handler.Error(c, stacktrace.Propagate(err, ""))
			return
		}
		if request.Purpose == ente.SignUpOTTPurpose && authHeader == "" {
			logrus.Info("UPAccessTokenAuthMiddleware: missing authorization header")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			return
		}
		if request.Purpose == ente.LoginOTTPurpose {
			c.Next()
		}

		// Check if token is cached
		cacheKey := authHeader
		username, found := m.Cache.Get(cacheKey)
		if !found {
			// Validate the token
			// The ValidateToken method already handles the "Bearer " prefix
			claims, err := m.JWTValidator.ValidateToken(authHeader)
			if err != nil {
				logrus.WithError(err).Info("UPAccessTokenAuthMiddleware: token validation failed")
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
				return
			}

			// Get preferred username from token
			if claims.PreferredUsername == "" {
				logrus.Info("UPAccessTokenAuthMiddleware: missing preferred username in token")
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing preferred username in token"})
				return
			}

			username = claims.PreferredUsername
			m.Cache.Set(cacheKey, username, cache.DefaultExpiration)
		}

		// Set the preferred username in the context
		c.Set(PreferredUsernameKey, username)
		logrus.Infof("UPAccessTokenAuthMiddleware: authenticated user with preferred username %s", username)
		c.Next()
	}
}
