package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/ente/museum/ente"
	"github.com/ente/museum/pkg/repo"
	"github.com/ente/museum/pkg/utils"
	"github.com/ente/stacktrace"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// UPStoreController reads a user's current subscription from the UP inner API.
type UPStoreController struct {
	UserRepo   *repo.UserRepository
	httpClient *http.Client
}

func NewUPStoreController(userRepo *repo.UserRepository) *UPStoreController {
	return &UPStoreController{
		UserRepo:   userRepo,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// GetVerifiedSubscription returns the UP subscription of the UP account userID.
func (c *UPStoreController) GetVerifiedSubscription(userID int64) (ente.UpSubscriptionDetails, error) {
	user, err := c.UserRepo.Get(userID)
	if err != nil {
		return ente.UpSubscriptionDetails{}, stacktrace.Propagate(err, "")
	}
	upUsername, ok := utils.UPUsernameFromEmail(user.Email)
	if !ok {
		return ente.UpSubscriptionDetails{}, stacktrace.Propagate(ente.ErrBadRequest, "user %d is not a UP account", userID)
	}
	return c.verifySubscriptionByUsername(upUsername)
}

func (c *UPStoreController) verifySubscriptionByUsername(username string) (ente.UpSubscriptionDetails, error) {
	endpoint := viper.GetString("unplugged.inner-api-host") + UnpluggedGetUserSubscriptionEndpoint + url.QueryEscape(username)
	resp, err := c.httpClient.Get(endpoint)
	if err != nil {
		return ente.UpSubscriptionDetails{}, stacktrace.Propagate(err, "failed to execute request")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ente.UpSubscriptionDetails{}, stacktrace.Propagate(
			fmt.Errorf("unexpected status code: %d", resp.StatusCode),
			"failed to verify subscription",
		)
	}
	var subscriptions []ente.UpSubscriptionDetails
	if err := json.NewDecoder(resp.Body).Decode(&subscriptions); err != nil {
		return ente.UpSubscriptionDetails{}, stacktrace.Propagate(err, "failed to decode response")
	}
	if len(subscriptions) == 0 {
		return ente.UpSubscriptionDetails{}, stacktrace.Propagate(ente.ErrNotFound, "no UP subscription for %s", username)
	}
	log.Infof("UP subscription for %s: type %s", username, subscriptions[0].Type)
	return subscriptions[0], nil
}
