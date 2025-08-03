package middleware

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"

	"github.com/ente-io/museum/pkg/utils/handler"
	"github.com/ente-io/stacktrace"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// WebhookSignatureMiddleware verifies the signature of incoming webhook requests
type WebhookSignatureMiddleware struct {
	Secret string
}

// VerifySignature returns a middleware that verifies the signature of the request body
// The signature is expected to be in the X-Webhook-Signature header
// The signature is generated using HMAC-SHA256 with the provided secret
func (m *WebhookSignatureMiddleware) VerifySignature() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get the signature from the header
		signature := c.GetHeader("X-Webhook-Signature")
		if signature == "" {
			logrus.Error("Missing webhook signature")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Missing signature"})
			return
		}

		// Read the request body
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			handler.Error(c, stacktrace.Propagate(err, "Failed to read request body"))
			return
		}

		// Restore the request body for subsequent handlers
		c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

		// Verify the signature
		expectedSignature, err := generateSignature(string(body), m.Secret)
		if err != nil {
			handler.Error(c, stacktrace.Propagate(err, "Failed to generate signature"))
			return
		}

		if signature != expectedSignature {
			logrus.WithFields(logrus.Fields{
				"expected": expectedSignature,
				"received": signature,
			}).Error("Invalid webhook signature")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid signature"})
			return
		}

		// Signature is valid, proceed to the next handler
		c.Next()
	}
}

// generateSignature generates a signature for the given payload using HMAC-SHA256
func generateSignature(payload, secret string) (string, error) {
	h := hmac.New(sha256.New, []byte(secret))
	_, err := h.Write([]byte(payload))
	if err != nil {
		return "", stacktrace.Propagate(err, "Failed to write payload to HMAC")
	}
	return base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}
