package user

import (
	"github.com/ente/museum/ente"
	"github.com/ente/museum/pkg/utils/auth"
	emailUtil "github.com/ente/museum/pkg/utils/email"
	"github.com/ente/stacktrace"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// UPOnVerificationSuccess logs in a user whose identity was already verified
// by the UP access token, without an email OTT or SRP exchange.
func (c *UserController) UPOnVerificationSuccess(context *gin.Context, email string, source *string) (ente.EmailAuthorizationResponse, error) {
	return c.onVerificationSuccess(context, email, source, nil)
}

// UPChangeEmail accepts the OTT for either the bare UP username or
// <username>@<unplugged.email-host>, then updates the account email.
func (c *UserController) UPChangeEmail(ctx *gin.Context, request ente.EmailVerificationRequest) error {
	username := emailUtil.NormalizeEmail(request.Email)
	log.Infof("Changing email to %s", username)
	err := c.verifyEmailOtt(ctx, username, request.OTT)
	if err != nil {
		emailUsername := username + "@" + viper.GetString("unplugged.email-host")
		err = c.verifyEmailOtt(ctx, emailUsername, request.OTT)
		if err != nil {
			return stacktrace.Propagate(err, "")
		}
	}
	return c.UpdateEmail(ctx, auth.GetUserID(ctx.Request.Header), username)
}
