package user

import (
	"github.com/ente/museum/ente"
	util "github.com/ente/museum/pkg/utils"
	"github.com/ente/museum/pkg/utils/auth"
	emailUtil "github.com/ente/museum/pkg/utils/email"
	"github.com/ente/stacktrace"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

// UPOnVerificationSuccess logs in a user whose identity was already verified
// by the UP access token, without an email OTT or SRP exchange.
func (c *UserController) UPOnVerificationSuccess(context *gin.Context, email string, source *string) (ente.EmailAuthorizationResponse, error) {
	return c.onVerificationSuccess(context, email, source, nil)
}

// UPChangeEmail changes the caller's account email to the one
// util.UPChangeEmailTarget allows. request.Email may be a bare UP username or
// a full email; a UP account always ends up with <username>@<email-host>,
// never the bare username, so utils.User.GetUserID can still resolve it.
func (c *UserController) UPChangeEmail(ctx *gin.Context, request ente.EmailVerificationRequest) error {
	userID := auth.GetUserID(ctx.Request.Header)
	user, err := c.UserRepo.Get(userID)
	if err != nil {
		return stacktrace.Propagate(err, "")
	}
	email, err := util.UPChangeEmailTarget(emailUtil.NormalizeEmail(user.Email), emailUtil.NormalizeEmail(request.Email))
	if err != nil {
		return stacktrace.Propagate(err, "")
	}
	log.Infof("Changing email of user %d", userID)
	if err := c.verifyEmailOtt(ctx, email, request.OTT); err != nil {
		return stacktrace.Propagate(err, "")
	}
	return c.UpdateEmail(ctx, userID, email)
}
