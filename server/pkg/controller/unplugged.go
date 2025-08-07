package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ente-io/museum/ente"
	"github.com/ente-io/museum/pkg/controller/commonbilling"
	"github.com/ente-io/museum/pkg/repo"
	"github.com/ente-io/stacktrace"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type UPStoreController struct {
	BillingRepo            *repo.BillingRepository
	FileRepo               *repo.FileRepository
	UserRepo               *repo.UserRepository
	BillingPlansPerCountry ente.BillingPlansPerCountry
	CommonBillCtrl         *commonbilling.Controller
}

// Return a new instance of UPStoreController
func NewUPStoreController(
	billingRepo *repo.BillingRepository,
	fileRepo *repo.FileRepository,
	userRepo *repo.UserRepository,
	commonBillCtrl *commonbilling.Controller,
) *UPStoreController {

	return &UPStoreController{
		BillingRepo:    billingRepo,
		FileRepo:       fileRepo,
		UserRepo:       userRepo,
		CommonBillCtrl: commonBillCtrl,
	}
}

// GetVerifiedSubscription verifies and returns the verified subscription
func (c *UPStoreController) GetVerifiedSubscription(userID int64) (ente.UpSubscriptionDetails, error) {

	user, err := c.UserRepo.Get(userID)
	var upUsername = user.Email
	log.Info("UP username: ", upUsername)
	if at := strings.Index(upUsername, "@"); at != -1 {
		upUsername = upUsername[:at]
		log.Info("UP username: ", upUsername)
	}

	response, err := c.verifySubscriptionByUsername(upUsername)
	log.Info("c.verifySubscriptionByUsername(upUsername) response: ", response)
	if err != nil {
		return ente.UpSubscriptionDetails{}, stacktrace.Propagate(err, "")
	}

	return response, nil
}

func (c *UPStoreController) verifySubscriptionByUsername(username string) (ente.UpSubscriptionDetails, error) {
	urlSubscriptionInner := viper.GetString("unplugged.inner-api-host")
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	req, err := http.NewRequest("GET", urlSubscriptionInner+UnpluggedGetUserSubscriptionEndpoint+username, nil)
	if err != nil {
		return ente.UpSubscriptionDetails{}, stacktrace.Propagate(err, "failed to create request")
	}

	resp, err := client.Do(req)
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
	log.Info("UP response: ", resp.Body)
	var newUPSubscription []ente.UpSubscriptionDetails
	err = json.NewDecoder(resp.Body).Decode(&newUPSubscription)
	if err != nil {
		return ente.UpSubscriptionDetails{}, stacktrace.Propagate(err, "failed to decode response")
	}
	return newUPSubscription[0], nil
}
