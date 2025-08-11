package controller

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"github.com/ente-io/museum/pkg/controller/commonbilling"
	"github.com/ente-io/museum/pkg/utils/billing"
	"github.com/ente-io/museum/pkg/utils/crypto"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/ente-io/museum/ente"
	"github.com/ente-io/museum/pkg/repo"
	"github.com/ente-io/stacktrace"
)

const (

	// Unplugged API endpoints
	UnpluggedGetUserSubscriptionEndpoint = "/inner/v2/by-username?username="
)

// UpSubscriptionDetails represents the response from Unplugged subscription details endpoint

// UPBillingController provides abstractions for handling Unplugged billing related queries
type UPBillingController struct {
	BillingRepo       *repo.BillingRepository
	UserRepo          *repo.UserRepository
	UsageRepo         *repo.UsageRepository
	UPStoreController *UPStoreController
	CommonBillCtrl    *commonbilling.Controller
	HashingKey        []byte
}

// NewUPBillingController returns a new instance of UPBillingController
func NewUPBillingController(
	billingRepo *repo.BillingRepository,
	userRepo *repo.UserRepository,
	usageRepo *repo.UsageRepository,
	upStoreController *UPStoreController,
	hashingKey []byte,
) *UPBillingController {
	return &UPBillingController{
		BillingRepo:       billingRepo,
		UserRepo:          userRepo,
		UsageRepo:         usageRepo,
		UPStoreController: upStoreController,
		HashingKey:        hashingKey,
	}
}

// UPVerifySubscription verifies and returns the verified subscription
func (c *UPBillingController) UPVerifySubscription(
	userID int64) (ente.Subscription, error) {
	// Basic (free) Products ID
	FreePlanProductID := viper.GetString("unplugged.basic-plan-id")

	var newSubscription ente.Subscription

	newUPSubscription, err := c.UPStoreController.GetVerifiedSubscription(userID)
	if err != nil {
		return ente.Subscription{}, stacktrace.Propagate(err, "failed to get verified subscription")
	}
	log.Infof("New subscription details newUPSubscription.Type: %s", newUPSubscription.Type)

	currentEnteSubscription, errEnteSub := c.BillingRepo.GetUserSubscription(userID)
	if errEnteSub != nil {
		return ente.Subscription{}, stacktrace.Propagate(err, "")
	}
	log.Infof("Current subscription details currenSubscription.Type: %s", currentEnteSubscription.ProductID)

	if newUPSubscription.Type == ente.FreePlanID {

		if currentEnteSubscription.ProductID == ente.FreePlanProductID || currentEnteSubscription.ProductID == FreePlanProductID {
			return currentEnteSubscription, nil
		} else {
			newFreeSubscription := billing.GetFreeSubscription(userID)
			err = c.BillingRepo.ReplaceSubscription(
				currentEnteSubscription.ID,
				newFreeSubscription,
			)
			if err != nil {
				return ente.Subscription{}, stacktrace.Propagate(err, "")
			}
			log.Info("Replaced subscription")
			newSubscription.ID = currentEnteSubscription.ID

			log.Info("Returning new subscription with ID " + strconv.FormatInt(newSubscription.ID, 10))
			return newSubscription, nil
		}

	}

	convertSubscription(userID, &newSubscription, &newUPSubscription, currentEnteSubscription)

	newSubscriptionExpiresSooner := newSubscription.ExpiryTime < currentEnteSubscription.ExpiryTime
	isUpgradingFromFreePlan := currentEnteSubscription.ProductID == ente.FreePlanProductID
	hasChangedProductID := currentEnteSubscription.ProductID != newSubscription.ProductID
	isOutdatedPurchase := !isUpgradingFromFreePlan && !hasChangedProductID && newSubscriptionExpiresSooner

	if isOutdatedPurchase {
		// User is reporting an outdated purchase that was already verified
		// no-op
		log.Info("Outdated purchase reported")
		return currentEnteSubscription, nil
	}
	if newSubscription.Storage < currentEnteSubscription.Storage {
		canDowngrade, canDowngradeErr := c.CommonBillCtrl.CanDowngradeToGivenStorage(newSubscription.Storage, userID)
		if canDowngradeErr != nil {
			return ente.Subscription{}, stacktrace.Propagate(canDowngradeErr, "")
		}
		if !canDowngrade {
			return ente.Subscription{}, stacktrace.Propagate(ente.ErrCannotDowngrade, "")
		}
		log.Info("Usage is good")
	}
	if newSubscription.OriginalTransactionID != "" && newSubscription.OriginalTransactionID != "none" {
		existingSub, existingSubErr := c.BillingRepo.GetSubscriptionForTransaction(newSubscription.OriginalTransactionID, ente.Unplugged)
		if existingSubErr != nil {
			if errors.Is(existingSubErr, sql.ErrNoRows) {
				log.Info("No subscription created yet")
			} else {
				log.Info("Something went wrong")
				log.WithError(existingSubErr).Error("GetSubscriptionForTransaction failed")
				return ente.Subscription{}, stacktrace.Propagate(existingSubErr, "")
			}
		} else {
			if existingSub.UserID != userID {
				log.WithFields(log.Fields{
					"original_transaction_id": existingSub.OriginalTransactionID,
					"existing_user":           existingSub.UserID,
					"current_user":            userID,
				}).Error("Subscription for given transactionID is attached with different user")
				log.Info("Subscription attached to different user")
				return ente.Subscription{}, stacktrace.Propagate(&ente.ErrSubscriptionAlreadyClaimed,
					fmt.Sprintf("Subscription with txn id %s already associated with user %d", newSubscription.OriginalTransactionID, existingSub.UserID))
			}
		}
	}
	err = c.BillingRepo.ReplaceSubscription(
		currentEnteSubscription.ID,
		newSubscription,
	)
	if err != nil {
		return ente.Subscription{}, stacktrace.Propagate(err, "")
	}
	log.Info("Replaced subscription")
	newSubscription.ID = currentEnteSubscription.ID

	log.Info("Returning new subscription with ID " + strconv.FormatInt(newSubscription.ID, 10))
	return newSubscription, nil
}

func convertSubscription(userID int64, newSubscription *ente.Subscription, newUPSubscription *ente.UpSubscriptionDetails,
	currentSubscription ente.Subscription) {
	newSubscription.ID = currentSubscription.ID
	newSubscription.Period = newUPSubscription.Interval
	newSubscription.PaymentProvider = ente.Unplugged
	newSubscription.UserID = userID
	newSubscription.ExpiryTime = newUPSubscription.ExpirationDate.UnixNano() / 1000
	newSubscription.ProductID = newUPSubscription.Price
	newSubscription.Storage = ente.PremiumPlanStorage
	newSubscription.OriginalTransactionID = newUPSubscription.ID
	newSubscription.Price = newUPSubscription.Price
	newSubscription.Attributes.IsCancelled = newUPSubscription.EndReason == "CANCELED"
}

// HandleSubscriptionWebhook processes webhook notifications for subscription events
func (c *UPBillingController) HandleSubscriptionWebhook(reqBody *ente.WebhookRequest) error {
	// Parse the webhook payload

	// Extract event type and subscription data
	event := reqBody.Event

	log.WithFields(log.Fields{
		"payload": event,
	}).Info("Received subscription webhook")

	// Process different event types

	err := c.handleSubscriptionUpdated(reqBody)
	if err != nil {
		return err
	}
	return nil
}

// handleSubscriptionUpdated processes subscription.updated events
func (c *UPBillingController) handleSubscriptionUpdated(reqBody *ente.WebhookRequest) error {
	// Extract user ID and updated subscription details from the webhook data
	// and update the user's subscription in the database
	log.Info("Processing subscription.updated event")

	// Get username from webhook request
	username := reqBody.Username
	log.Infof("reqBody.Username: %s", username)
	// Try to find user by username
	emailHash, err := crypto.GetHash(username, c.HashingKey)
	log.Infof("reqBody.Username: %s", username)
	if err != nil {
		return stacktrace.Propagate(err, "failed to hash username")
	}

	user, errShort := c.UserRepo.GetUserByEmailHash(emailHash)

	if errShort != nil {
		// If user not found, try with email format (username@domain)
		log.Infof("user not found c.UserRepo.GetUserByEmailHash(emailHash): %s", err)

		emailUsername := username + "@" + viper.GetString("unplugged.email-host")
		emailHashAlt, errAlt := crypto.GetHash(emailUsername, c.HashingKey)
		if errAlt != nil {
			return stacktrace.Propagate(err, "failed to hash email username")
		}
		emailUser, errUser := c.UserRepo.GetUserByEmailHash(emailHashAlt)
		if errUser != nil {
			log.Errorf("emailUser not found c.UserRepo.GetUserByEmailHash(emailHashAlt): %s", err)
			return stacktrace.Propagate(err, "failed to get user by emailUser hash")
		}
		log.Infof("emailUser username: %s, ID: %s", emailUser.Email, emailUser.ID)
		_, errVerify := c.UPVerifySubscription(emailUser.ID)
		if errVerify != nil {
			log.Errorf("failed to verify subscription for emailUser: %s, Err: %s", emailUser.Email, err)
			return stacktrace.Propagate(err, "failed to verify subscription for emailUser")
		}
	} else {
		// Verify and update the subscription for the user
		_, err = c.UPVerifySubscription(user.ID)
		if err != nil {
			log.Errorf("failed to verify subscription for user: %s, Err: %s", user.Email, err)
			return stacktrace.Propagate(err, "failed to verify subscription for user")
		}
	}

	return nil
}
