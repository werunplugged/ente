package api

import (
	"errors"
	"net/http"

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

// SendOTT signs in (or signs up) the UP user named by the access token that
// UPAccessTokenMiddleware validated, without asking for an emailed OTT.
func (h *UPUserHandler) SendOTT(c *gin.Context) {
	var request ente.SendOTTRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.Error(c, stacktrace.Propagate(err, ""))
		return
	}
	if request.Purpose != ente.SignUpOTTPurpose && request.Purpose != ente.LoginOTTPurpose {
		log.Errorf("Current OTT Purpose: %s. It must be %s or %s", request.Purpose, ente.SignUpOTTPurpose, ente.LoginOTTPurpose)
		handler.Error(c, stacktrace.Propagate(ente.ErrBadRequest, "Invalid OTT purpose"))
		return
	}

	// Set by UPAccessTokenMiddleware from the token's preferred_username.
	username := c.GetHeader(middleware.UpUsernameHeader)
	email, err := utils.UPEmail(username)
	if err != nil {
		handler.Error(c, stacktrace.Propagate(err, ""))
		return
	}
	// 0 when the username has no museum account yet.
	userID := auth.GetUserID(c.Request.Header)
	if userID != 0 && request.Purpose == ente.SignUpOTTPurpose {
		log.Warningf("SendOTT Trying to send OTT for logged userID %d, username %s", userID, username)
		handler.Error(c, stacktrace.Propagate(ente.ErrUserAlreadyRegistered, "user has already completed sign up process"))
		return
	}
	log.Infof("SendOTT, Sending OTT for %s", request.Purpose)

	// Other SendEmailOTT errors are ignored, as before this change: the access
	// token already proves the identity, so the emailed OTT is not needed.
	err = h.UserController.SendEmailOTT(c, email, request.Purpose, request.Mobile)
	if errors.Is(err, ente.ErrUserSignupIncomplete) {
		// Delete the incomplete account so it can be created again. Resolve it
		// from the same email SendEmailOTT just checked.
		incompleteID, _, err := h.UserUtils.GetUserID(username)
		if err != nil {
			handler.Error(c, stacktrace.Propagate(err, "failed to resolve incomplete account"))
			return
		}
		logger := log.WithFields(log.Fields{
			"user_id":    incompleteID,
			"user_email": email,
			"req_id":     requestid.Get(c),
			"req_ctx":    "account_deletion",
		})
		if _, err := h.UserController.HandleAccountDeletion(c, incompleteID, logger); err != nil {
			handler.Error(c, stacktrace.Propagate(err, ""))
			return
		}
		isCancelled, err := h.UserController.BillingController.UPHandleAccountDeletion(incompleteID, logger)
		if err != nil {
			handler.Error(c, stacktrace.Propagate(err, ""))
			return
		}
		if !isCancelled {
			handler.Error(c, stacktrace.Propagate(ente.ErrBadRequest, "subscription of incomplete account was not cancelled"))
			return
		}
	}

	source := "UP Store"
	emailHash, err := crypto.GetHash(email, h.UserController.HashingKey)
	if err != nil {
		handler.Error(c, stacktrace.Propagate(err, ""))
		return
	}
	app := auth.GetApp(c)
	otts, err := h.UserController.UserAuthRepo.GetValidOTTs(emailHash, app)
	if err != nil {
		handler.Error(c, stacktrace.Propagate(err, ""))
		return
	}
	if len(otts) > 0 {
		_, err := h.UserController.UserAuthRepo.RemoveOTT(emailHash, otts[0], app)
		if err != nil {
			handler.Error(c, stacktrace.Propagate(err, "failed to remove previous OTT"))
			return
		}
	}
	response, err := h.UserController.UPOnVerificationSuccess(c, email, &source)
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
