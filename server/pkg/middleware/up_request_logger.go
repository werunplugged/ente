package middleware

import (
	"github.com/gin-gonic/gin"
)

// UPLogger wraps Logger and skips health-check probes, which would otherwise
// flood the logs.
func UPLogger(urlSanitizer func(_ *gin.Context) string) gin.HandlerFunc {
	logger := Logger(urlSanitizer)
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/ping" || c.Request.URL.Path == "/fire/db-m-ping" {
			c.Next()
			return
		}
		logger(c)
	}
}
