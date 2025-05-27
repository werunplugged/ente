package api

import (
	"github.com/ente-io/museum/ente"
	"github.com/ente-io/museum/pkg/controller/user"
	"github.com/ente-io/museum/pkg/utils/auth"
	"github.com/ente-io/museum/pkg/utils/handler"
	"github.com/ente-io/stacktrace"
	"github.com/gin-gonic/gin"
	"net/http"
)

// UPUserHandler handles user-related requests for the UP API
type UPUserHandler struct {
	UserController *user.UserController
	JWTValidator   *auth.JWTValidator
}

// SendOTT validates the JWT token and then calls the original SendOTT method
func (h *UPUserHandler) SendOTT(c *gin.Context) {
	// Validate JWT token
	authToken := c.GetHeader("Authorization")
	if authToken == "" {
		handler.Error(c, stacktrace.Propagate(ente.NewBadRequestWithMessage("Authorization header is required"), ""))
		return
	}

	// Validate the token
	_, err := h.JWTValidator.ValidateToken(authToken)
	if err != nil {
		handler.Error(c, stacktrace.Propagate(err, "JWT validation failed"))
		return
	}

	var request ente.SendOTTRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.Error(c, stacktrace.Propagate(err, ""))
		return
	}
	username, err := h.JWTValidator.GetPreferredUsername(authToken)
	if len(username) == 0 {
		handler.Error(c, stacktrace.Propagate(ente.ErrBadRequest, "Email id is missing"))
		return
	}
	err = h.UserController.SendEmailOTT(c, username, request.Purpose)
	if err != nil {
		handler.Error(c, stacktrace.Propagate(err, ""))
		return
	} else {
		c.Status(200)
	}
}

// VerifyEmail validates the JWT token, extracts the username from it, and then calls the original VerifyEmail method
func (h *UPUserHandler) VerifyEmail(c *gin.Context) {
	// Validate JWT token
	authToken := c.GetHeader("Authorization")
	if authToken == "" {
		handler.Error(c, stacktrace.Propagate(ente.NewBadRequestWithMessage("Authorization header is required"), ""))
		return
	}

	// Validate the token
	_, err := h.JWTValidator.ValidateToken(authToken)
	if err != nil {
		handler.Error(c, stacktrace.Propagate(err, "JWT validation failed"))
		return
	}

	// Get username from token
	username, err := h.JWTValidator.GetPreferredUsername(authToken)
	if err != nil {
		handler.Error(c, stacktrace.Propagate(err, "Failed to get username from token"))
		return
	}

	var request ente.EmailVerificationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.Error(c, stacktrace.Propagate(err, ""))
		return
	}

	// Override email in request with username from token
	request.Email = username

	response, err := h.UserController.OnVerificationSuccess(c, request.Email, request.Source)
	if err != nil {
		handler.Error(c, stacktrace.Propagate(err, ""))
		return
	}
	c.JSON(http.StatusOK, response)
}
