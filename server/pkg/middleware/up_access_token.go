package middleware

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/ente/museum/ente"
	"github.com/ente/museum/pkg/utils"
	"github.com/ente/museum/pkg/utils/auth"
	"github.com/gin-gonic/gin"
	"github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
)

// UpUsernameHeader carries the token's preferred_username (the bare UP
// username, not the account email) to the handlers.
const UpUsernameHeader = "X-UP-Username"

// AuthUserID carries the museum user ID for the token's username. It is
// absent when no museum account exists yet for that username (sign-up).
const AuthUserID = "X-Auth-User-ID"

// upRolesKey is the gin context key holding the token's Keycloak realm roles.
const upRolesKey = "up-realm-roles"

// maxTokenCacheTTL bounds how long a validated token is trusted without
// re-checking it.
const maxTokenCacheTTL = 60 * time.Second

// UPAccessTokenMiddleware intercepts and authenticates incoming requests using JWT tokens
type UPAccessTokenMiddleware struct {
	JWTValidator *auth.JWTValidator
	Cache        *cache.Cache
	UserUtils    *utils.User
	// AdminRoles are the Keycloak realm roles allowed on /up-admin. Empty
	// means no one is allowed.
	AdminRoles []string
}

type upTokenInfo struct {
	username string
	roles    []string
}

// UPAccessTokenAuthMiddleware validates the Authorization bearer token and
// sets UpUsernameHeader, plus AuthUserID when the username has a museum
// account. Any client-supplied AuthUserID header is discarded.
func (m *UPAccessTokenMiddleware) UPAccessTokenAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Header.Del(AuthUserID)
		c.Request.Header.Del(UpUsernameHeader)

		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			logrus.Info("UPAccessTokenAuthMiddleware: missing authorization header")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			return
		}

		var info upTokenInfo
		if cached, found := m.Cache.Get(authHeader); found {
			info = cached.(upTokenInfo)
		} else {
			claims, err := m.JWTValidator.ValidateToken(authHeader)
			if err != nil {
				logrus.WithError(err).Info("UPAccessTokenAuthMiddleware: token validation failed")
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
				return
			}
			if claims.PreferredUsername == "" {
				logrus.Info("UPAccessTokenAuthMiddleware: missing preferred username in token")
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing preferred username in token"})
				return
			}
			info = upTokenInfo{username: claims.PreferredUsername, roles: claims.RealmAccess.Roles}
			// The validator requires exp, so ExpiresAt is set.
			ttl := min(time.Until(claims.ExpiresAt.Time), maxTokenCacheTTL)
			if ttl > 0 {
				m.Cache.Set(authHeader, info, ttl)
			}
		}

		userID, _, err := m.UserUtils.GetUserID(info.username)
		switch {
		case err == nil:
			c.Request.Header.Set(AuthUserID, strconv.FormatInt(userID, 10))
		case errors.Is(err, ente.ErrNotFound):
			// No museum account yet: leave AuthUserID unset.
		case errors.Is(err, ente.ErrBadRequest):
			logrus.WithError(err).Info("UPAccessTokenAuthMiddleware: invalid preferred username in token")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid preferred username in token"})
			return
		default:
			logrus.WithError(err).Error("UPAccessTokenAuthMiddleware: failed to resolve user")
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve user"})
			return
		}
		c.Request.Header.Set(UpUsernameHeader, info.username)
		c.Set(upRolesKey, info.roles)
		logrus.Infof("UPAccessTokenAuthMiddleware: authenticated user with preferred username %s", info.username)
		c.Next()
	}
}

// UPAdminRoleMiddleware allows the request only if the UP access token (already
// validated by UPAccessTokenAuthMiddleware) carries one of m.AdminRoles.
func (m *UPAccessTokenMiddleware) UPAdminRoleMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		roles, _ := c.Value(upRolesKey).([]string)
		for _, role := range roles {
			if slices.Contains(m.AdminRoles, role) {
				c.Next()
				return
			}
		}
		logrus.Warnf("UPAdminRoleMiddleware: %s has no UP admin role", c.Request.Header.Get(UpUsernameHeader))
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
	}
}
