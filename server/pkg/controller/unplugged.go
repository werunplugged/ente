package controller

import (
	"encoding/json"
	"fmt"
	"github.com/ente-io/museum/ente"
	"github.com/ente-io/museum/pkg/controller/commonbilling"
	"github.com/ente-io/museum/pkg/repo"
	"github.com/ente-io/stacktrace"
	"github.com/spf13/viper"
	"net/http"
	"strings"
	"time"
)

type UPStoreController struct {
	BillingRepo            *repo.BillingRepository
	FileRepo               *repo.FileRepository
	UserRepo               *repo.UserRepository
	BillingPlansPerCountry ente.BillingPlansPerCountry
	CommonBillCtrl         *commonbilling.Controller
}

const UPStorePackageName = "io.up.ente.photos"

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
func (c *UPStoreController) GetVerifiedSubscription(userID int64) (UpSubscriptionDetails, error) {

	user, err := c.UserRepo.Get(userID)
	var upUsername = user.Email
	if at := strings.Index(upUsername, "@"); at != -1 {
		upUsername = upUsername[:at]
	}

	response, err := c.verifySubscriptionByUsername(upUsername)
	if err != nil {
		return UpSubscriptionDetails{}, stacktrace.Propagate(err, "")
	}

	return response, nil
}

func (c *UPStoreController) verifySubscriptionByUsername(username string) (UpSubscriptionDetails, error) {
	urlSubscriptionInner := viper.GetString("unplugged.inner-api-host")
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	req, err := http.NewRequest("GET", urlSubscriptionInner+UnpluggedGetUserSubscriptionEndpoint+username, nil)
	if err != nil {
		return UpSubscriptionDetails{}, stacktrace.Propagate(err, "failed to create request")
	}

	resp, err := client.Do(req)
	if err != nil {
		return UpSubscriptionDetails{}, stacktrace.Propagate(err, "failed to execute request")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return UpSubscriptionDetails{}, stacktrace.Propagate(
			fmt.Errorf("unexpected status code: %d", resp.StatusCode),
			"failed to verify subscription",
		)
	}

	var newUPSubscription []UpSubscriptionDetails
	err = json.NewDecoder(resp.Body).Decode(&newUPSubscription)
	if err != nil {
		return UpSubscriptionDetails{}, stacktrace.Propagate(err, "failed to decode response")
	}
	return newUPSubscription[0], nil
}
