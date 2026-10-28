package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/ente/museum/pkg/controller"
	"github.com/ente/museum/pkg/middleware"
	"github.com/gin-contrib/requestid"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/ente/museum/ente"
	"github.com/ente/museum/pkg/controller/user"
	"github.com/ente/museum/pkg/utils"
	"github.com/ente/museum/pkg/utils/auth"
	"github.com/ente/museum/pkg/utils/crypto"
	"github.com/ente/museum/pkg/utils/handler"
	"github.com/ente/stacktrace"
	"github.com/gin-gonic/gin"
)

// UPUserHandler handles user-related requests for the UP API
type UPUserHandler struct {
	UserController      *user.UserController
	JWTValidator        *auth.JWTValidator
	UPBillingController *controller.UPBillingController
	UPStoreController   *controller.UPStoreController
	UserUtils           *utils.User
}

// VerifyEmail verifies the OTT sent to the UP username's mailbox
// (<username>@<unplugged.email-host>) and logs the user in.
func (h *UPUserHandler) VerifyEmail(c *gin.Context) {
	var request ente.EmailVerificationRequest
	if err := handler.BindJSON(c, &request); err != nil {
		handler.Error(c, stacktrace.Propagate(err, ""))
		return
	}
	request.Email = request.Email + "@" + viper.GetString("unplugged.email-host")
	response, err := h.UserController.VerifyEmail(c, request)
	if err != nil {
		handler.Error(c, stacktrace.Propagate(err, ""))
		return
	}
	c.JSON(http.StatusOK, response)
}

// ChangeEmail accepts the OTT for either the bare UP username or
// <username>@<unplugged.email-host>.
func (h *UPUserHandler) ChangeEmail(c *gin.Context) {
	var request ente.EmailVerificationRequest
	if err := handler.BindJSON(c, &request); err != nil {
		handler.Error(c, stacktrace.Propagate(err, ""))
		return
	}
	err := h.UserController.UPChangeEmail(c, request)
	if err != nil {
		handler.Error(c, stacktrace.Propagate(err, ""))
		return
	}
	c.Status(http.StatusOK)
}

// GetSRPAttributes resolves a UP username to the account email before
// looking up SRP attributes. Unknown usernames fall through unchanged.
func (h *UPUserHandler) GetSRPAttributes(c *gin.Context) {
	var request ente.GetSRPAttributesRequest
	if err := c.ShouldBindQuery(&request); err != nil {
		handler.Error(c,
			stacktrace.Propagate(ente.ErrBadRequest, "Request binding failed %s", err))
		return
	}
	email := request.Email
	if _, resolved, err := h.UserUtils.GetUserID(request.Email); err == nil {
		email = resolved
	}
	response, err := h.UserController.GetSRPAttributes(c, email)
	if err != nil {
		handler.Error(c, stacktrace.Propagate(err, ""))
		return
	}
	log.WithFields(log.Fields{
		"email":       email,
		"srp_user_id": response.SRPUserID,
	}).Info("Sending SRP attributes")
	c.JSON(http.StatusOK, gin.H{"attributes": response})
}

// SendOTT validates the JWT token and then calls the original SendOTT method
func (h *UPUserHandler) SendOTT(c *gin.Context) {

	var request ente.SendOTTRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.Error(c, stacktrace.Propagate(err, ""))
		return
	}

	userID, err := strconv.ParseInt(c.Request.Header.Get(middleware.AuthUserID), 10, 64)
	if userID != 0 && request.Purpose == ente.SignUpOTTPurpose {
		log.Warningf("SendOTT Trying to send OTT for logged userID %d, email %s",
			userID, c.Request.Header.Get(middleware.UpUsernameHeader))
		handler.Error(c, stacktrace.Propagate(ente.ErrUserAlreadyRegistered, "user has already completed sign up process"))
		return
	}
	// Validate JWT token
	authToken := c.GetHeader("Authorization")

	log.Infof("SendOTT, Sending OTT for %s", request.Purpose)
	preferredUsername, _ := h.JWTValidator.GetPreferredUsername(authToken)
	username, isError := buildEmailFromUsername(c, preferredUsername)
	if isError {
		return
	}

	if request.Purpose == ente.SignUpOTTPurpose || request.Purpose == ente.LoginOTTPurpose {
		err := h.UserController.SendEmailOTT(c, username, request.Purpose, request.Mobile)
		if err != nil {
			if strings.Contains(err.Error(), "user has not completed sign up process") {
				logger := log.WithFields(log.Fields{
					"user_id":    userID,
					"user_email": username,
					"req_id":     requestid.Get(c),
					"req_ctx":    "account_deletion",
				})
				_, err := h.UserController.HandleAccountDeletion(c, userID, logger)
				if err != nil {
					handler.Error(c, stacktrace.Propagate(err, ""))
					return
				}
				isCancelled, err := h.UserController.BillingController.UPHandleAccountDeletion(userID, logger)
				if err != nil || isCancelled == false {
					handler.Error(c, stacktrace.Propagate(err, ""))
					return
				}
			}
		}
	} else {
		log.Errorf("Current OTT Purpose: %s. It must be %s", request.Purpose, ente.SignUpOTTPurpose)
		handler.Error(c, stacktrace.Propagate(ente.ErrBadRequest, "Invalid OTT purpose"))
		return
	}

	source := "UP Store"

	usernameHash, _ := crypto.GetHash(username, h.UserController.HashingKey)
	app := auth.GetApp(c)
	otts, _ := h.UserController.UserAuthRepo.GetValidOTTs(usernameHash, app)
	if len(otts) > 0 {
		_, err := h.UserController.UserAuthRepo.RemoveOTT(usernameHash, otts[0], app)
		if err != nil {
			handler.Error(c, stacktrace.Propagate(err, "failed to remove previous OTT"))
			return
		}
	}
	response, err := h.UserController.UPOnVerificationSuccess(c, username, &source)
	if err != nil {
		handler.Error(c, stacktrace.Propagate(err, "h.UserController.UPOnVerificationSuccess failed"))
		return
	}
	_, err = h.UPBillingController.UPVerifySubscription(response.ID)
	if err != nil {
		handler.Error(c, stacktrace.Propagate(err, "h.UPBillingController.UPVerifySubscription(response.ID) failed"))
		return
	}
	c.JSON(http.StatusOK, response)

}

func buildEmailFromUsername(c *gin.Context, preferredUsername string) (string, bool) {
	var emailHost = viper.GetString("unplugged.email-host")
	username := preferredUsername + "@" + emailHost
	log.Infof("OTT Username: %s", username)
	if len(username) == 0 {
		handler.Error(c, stacktrace.Propagate(ente.ErrBadRequest, "Email id is missing"))
		return "", true
	}
	return username, false
}
