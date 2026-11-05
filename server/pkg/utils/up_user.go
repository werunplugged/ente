package utils

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/ente/museum/ente"
	"github.com/ente/museum/pkg/repo"
	"github.com/ente/museum/pkg/utils/crypto"
	"github.com/ente/stacktrace"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type User struct {
	UserRepo   *repo.UserRepository
	HashingKey []byte
}

func upEmailSuffix() string {
	return "@" + viper.GetString("unplugged.email-host")
}

// UPEmail returns the museum account email for a UP username:
// <username>@<unplugged.email-host>. It rejects empty usernames and ones that
// already contain "@" or whitespace, so a username can never be read as some
// other account's email.
func UPEmail(username string) (string, error) {
	if username == "" || strings.ContainsAny(username, "@ \t\r\n") {
		return "", stacktrace.Propagate(ente.ErrBadRequest, "invalid UP username")
	}
	if viper.GetString("unplugged.email-host") == "" {
		return "", stacktrace.Propagate(errors.New("unplugged.email-host is not configured"), "")
	}
	return username + upEmailSuffix(), nil
}

// UPUsernameFromEmail is the inverse of UPEmail. It reports false when email
// is not a UP account email.
func UPUsernameFromEmail(email string) (string, bool) {
	username, ok := strings.CutSuffix(email, upEmailSuffix())
	if !ok || username == "" || strings.Contains(username, "@") {
		return "", false
	}
	return username, true
}

// GetUserID resolves a UP username to the museum user whose email is
// UPEmail(upUsername), returning that user's ID and email. It returns
// ente.ErrBadRequest for an invalid username and ente.ErrNotFound when no such
// user exists.
func (c *User) GetUserID(upUsername string) (int64, string, error) {
	email, err := UPEmail(upUsername)
	if err != nil {
		return 0, "", err
	}
	emailHash, err := crypto.GetHash(email, c.HashingKey)
	if err != nil {
		return 0, "", stacktrace.Propagate(err, "failed to hash UP email")
	}
	user, err := c.UserRepo.GetUserByEmailHash(emailHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, email, stacktrace.Propagate(ente.ErrNotFound, "no user for UP username")
		}
		return 0, "", stacktrace.Propagate(err, "failed to get user by UP email hash")
	}
	log.Debugf("UP username resolved to user %d", user.ID)
	return user.ID, email, nil
}
