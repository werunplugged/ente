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

// UPChangeEmailTarget returns the email a change-email request may set on an
// account, given the account's current email and the requested one (both
// already normalized). A requested value without "@" is read as a UP username.
//
// The email of a UP account is its identity key, so such an account may only
// be set to the canonical UPEmail of its own username. That is a real change
// only for legacy accounts stored under the bare username. Any other account
// keeps Ente's behaviour but may not take a UP email, which would claim a UP
// identity without a UP access token.
func UPChangeEmailTarget(current, requested string) (string, error) {
	target := requested
	if !strings.Contains(target, "@") {
		upEmail, err := UPEmail(target)
		if err != nil {
			return "", err
		}
		target = upEmail
	}
	_, targetIsUP := UPUsernameFromEmail(target)

	username, currentIsUP := UPUsernameFromEmail(current)
	if !strings.Contains(current, "@") {
		// Legacy UP account stored under the bare username.
		username, currentIsUP = current, true
	}
	if !currentIsUP {
		if targetIsUP {
			return "", stacktrace.Propagate(ente.ErrPermissionDenied, "a UP email can only be set through UP sign-in")
		}
		return target, nil
	}
	canonical, err := UPEmail(username)
	if err != nil {
		return "", err
	}
	if target != canonical {
		return "", stacktrace.Propagate(ente.ErrPermissionDenied, "the email of a UP account cannot be changed")
	}
	return target, nil
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
