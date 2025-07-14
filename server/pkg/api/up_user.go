package api

import (
	"net/http"

	"github.com/ente-io/museum/ente"
	"github.com/ente-io/museum/pkg/controller/user"
	"github.com/ente-io/museum/pkg/utils/auth"
	"github.com/ente-io/museum/pkg/utils/crypto"
	"github.com/ente-io/museum/pkg/utils/handler"
	"github.com/ente-io/stacktrace"
	"github.com/gin-gonic/gin"
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
		handler.Error(c, stacktrace.Propagate(err, "Error validating token: X"+authToken+"X"))
		return
	}

	var request ente.SendOTTRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.Error(c, stacktrace.Propagate(err, ""))
		return
	}
	username, _ := h.JWTValidator.GetPreferredUsername(authToken)
	if len(username) == 0 {
		handler.Error(c, stacktrace.Propagate(ente.ErrBadRequest, "Email id is missing"))
		return
	}
	if request.Purpose == ente.SignUpOTTPurpose {
		err = h.UserController.SendEmailOTT(c, username, request.Purpose)
		if err != nil {
			handler.Error(c, stacktrace.Propagate(err, ""))
			return
		}
	}
	source := "UP Store"

	usernameHash, _ := crypto.GetHash(username, h.UserController.HashingKey)
	otts, _ := h.UserController.UserAuthRepo.GetValidOTTs(usernameHash, auth.GetApp(c))
	app := auth.GetApp(c)

	h.UserController.UserAuthRepo.RemoveOTT(usernameHash, otts[0], app)
	response, err := h.UserController.OnVerificationSuccess(c, username, &source)
	if err != nil {
		handler.Error(c, stacktrace.Propagate(err, ""))
		return
	}
	c.JSON(http.StatusOK, response)

}
