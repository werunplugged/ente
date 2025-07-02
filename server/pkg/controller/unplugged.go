package controller

import (
	"encoding/json"
	"fmt"
	"github.com/awa/go-iap/playstore"
	"github.com/ente-io/museum/ente"
	"github.com/ente-io/museum/pkg/controller/commonbilling"
	"github.com/ente-io/museum/pkg/repo"
	"github.com/ente-io/museum/pkg/repo/storagebonus"
	"github.com/ente-io/museum/pkg/utils/config"
	"github.com/ente-io/stacktrace"
	"github.com/spf13/viper"
	"net/http"
	"os"
	"time"
)

type UPStoreController struct {
	BillingRepo            *repo.BillingRepository
	FileRepo               *repo.FileRepository
	UserRepo               *repo.UserRepository
	StorageBonusRepo       *storagebonus.Repository
	BillingPlansPerCountry ente.BillingPlansPerCountry
	CommonBillCtrl         *commonbilling.Controller
}

const UPStorePackageName = "io.up.ente.photos"

// Return a new instance of PlayStoreController
func NewUPStoreController(
	plans ente.BillingPlansPerCountry,
	billingRepo *repo.BillingRepository,
	fileRepo *repo.FileRepository,
	userRepo *repo.UserRepository,
	storageBonusRepo *storagebonus.Repository,
	commonBillCtrl *commonbilling.Controller,
) *UPStoreController {

	return &UPStoreController{
		BillingRepo:            billingRepo,
		FileRepo:               fileRepo,
		UserRepo:               userRepo,
		BillingPlansPerCountry: plans,
		StorageBonusRepo:       storageBonusRepo,
		CommonBillCtrl:         commonBillCtrl,
	}
}
func newUPStoreClient() (*playstore.Client, error) {
	playStoreCredentialsFile, err := config.CredentialFilePath("pst-service-account.json")
	if err != nil {
		return nil, stacktrace.Propagate(err, "")
	}
	if playStoreCredentialsFile == "" {
		// Can happen when running locally
		return nil, nil
	}

	jsonKey, err := os.ReadFile(playStoreCredentialsFile)
	if err != nil {
		return nil, stacktrace.Propagate(err, "")
	}
	playStoreClient, err := playstore.New(jsonKey)
	if err != nil {
		return nil, stacktrace.Propagate(err, "")
	}

	return playStoreClient, nil
}

// GetVerifiedSubscription verifies and returns the verified subscription
func (c *UPStoreController) GetVerifiedSubscription(userID int64) (ente.Subscription, error) {
	var s ente.Subscription

	user, err := c.UserRepo.Get(userID)

	response, err := c.verifySubscriptionByUsername(user.Email)
	if err != nil {
		return ente.Subscription{}, stacktrace.Propagate(err, "")
	}

	s.UserID = userID
	s.PaymentProvider = UnpluggedProvider
	s.ProductID = response.PriceId
	s.OriginalTransactionID = response.ID
	s.ExpiryTime = response.ExpirationDate.UnixMicro()
	return s, nil
}

func (c *UPStoreController) verifySubscriptionByUsername(username string) (UpSubscriptionDetails, error) {
	urlSubscriptionInner := viper.GetString("unplugged-subscription.inner-api-host")
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	req, err := http.NewRequest("GET", urlSubscriptionInner+GetUserSubscription+username, nil)
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

	var newSubscription UpSubscriptionDetails
	err = json.NewDecoder(resp.Body).Decode(&newSubscription)
	if err != nil {
		return UpSubscriptionDetails{}, stacktrace.Propagate(err, "failed to decode response")
	}
	return newSubscription, nil
}
