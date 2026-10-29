package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ente/museum/ente"
	"github.com/ente/museum/pkg/controller/discord"
	"github.com/ente/museum/pkg/controller/emergency"
	"github.com/ente/museum/pkg/controller/user"
	"github.com/ente/museum/pkg/repo"
	"github.com/ente/museum/pkg/utils"
	"github.com/ente/museum/pkg/utils/auth"
	"github.com/ente/museum/pkg/utils/handler"
	"github.com/ente/museum/pkg/utils/time"
	"github.com/ente/stacktrace"
	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// UPAdminHandler serves the /up-admin endpoints
type UPAdminHandler struct {
	UserRepo            *repo.UserRepository
	UserController      *user.UserController
	EmergencyController *emergency.Controller
	DiscordController   *discord.DiscordController
	UserUtils           *utils.User
}

func (h *UPAdminHandler) UPGetUsers(c *gin.Context) {

	sinceTime, err := strconv.ParseInt(c.Query("sinceTime"), 10, 64)
	if err != nil {
		handler.Error(c, stacktrace.Propagate(err, ""))
		return
	}
	users, err := h.UserRepo.GetAll(sinceTime, time.Microseconds())
	if err != nil {
		handler.Error(c, stacktrace.Propagate(err, ""))
		return
	}
	c.JSON(http.StatusOK, gin.H{"users": users})
}

func (h *UPAdminHandler) UPDeleteUser(context *gin.Context) {
	var username string
	username = context.Query("username")
	username = strings.TrimSpace(username)
	if username == "" {
		handler.Error(context, stacktrace.Propagate(ente.ErrBadRequest, "email id is missing"))
		return
	}
	userID, username, err := h.UserUtils.GetUserID(username)
	if err != nil {
		handler.Error(context, stacktrace.Propagate(err, "failed to resolve user to delete"))
		return
	}

	adminID := auth.GetUserID(context.Request.Header)
	logger := logrus.WithFields(logrus.Fields{
		"user_id":    userID,
		"admin_id":   adminID,
		"user_email": username,
		"req_id":     requestid.Get(context),
		"req_ctx":    "account_deletion",
	})

	removeLegacyErr := h.EmergencyController.HandleAccountDeletion(context, userID, logger)
	if removeLegacyErr != nil {
		handler.Error(context, stacktrace.Propagate(removeLegacyErr, ""))
		return
	}
	isCanceled, err := h.UserController.BillingController.UPHandleAccountDeletion(userID, logger)
	if err != nil || isCanceled == false {
		logrus.Error("Something went wrong with the subscription cancellation {} ", err)
		handler.Error(context, stacktrace.Propagate(err, ""))
		return
	}
	deleted, err := h.UserController.HandleAccountDeletion(context, userID, logger)
	if err != nil {
		logrus.Error("Something went wrong with the account deletion {} ", err)
		handler.Error(context, stacktrace.Propagate(err, ""))
		return
	}
	go h.DiscordController.NotifyAdminAction(
		fmt.Sprintf("Admin (%d) deleting account for %d", adminID, userID))
	context.JSON(http.StatusOK, deleted)
}
