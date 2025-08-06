package controller

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ente-io/museum/pkg/controller/commonbilling"
	"github.com/ente-io/museum/pkg/utils/crypto"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/ente-io/museum/ente"
	"github.com/ente-io/museum/pkg/repo"
	"github.com/ente-io/stacktrace"
)

const (
	// Storage sizes in bytes
	FreePlanStorage    int64 = 10 * 1024 * 1024 * 1024   // 10GB
	PremiumPlanStorage int64 = 1000 * 1024 * 1024 * 1024 // 1TB

	// Plan IDs
	FreePlanID    = "BASIC"
	PremiumPlanID = "PREMIUM"

	// Payment provider
	UnpluggedProvider ente.PaymentProvider = "unplugged"

	// Unplugged API endpoints
	UnpluggedSubscriptionDetailsEndpoint = "/api/subscriptions/v2/details"
	UnpluggedSubscriptionCancelEndpoint  = "/api/subscriptions/v2/cancel"
	UnpluggedGetUserSubscriptionEndpoint = "/inner/v2/by-username?username="
)

// UpSubscriptionDetails represents the response from Unplugged subscription details endpoint
type UpSubscriptionDetails struct {
	ID             string            `json:"subscriptionId"`
	Type           string            `json:"type"`     // enum BASIC, PREMIUM
	Interval       string            `json:"interval"` // YEAR, MONTH
	Price          string            `json:"priceId"`
	Currency       string            `json:"currency"`
	Provider       string            `json:"provider"` // UNPLUGGED, STRIPE
	Username       string            `json:"username"`
	ExpirationDate time.Time         `json:"expirationDate"`
	Metadata       map[string]string `json:"metadata"`
	EndReason      string            `json:"endReason"`
}

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

// GetPlans returns the available Unplugged subscription plans
func (c *UPBillingController) GetPlans() []ente.BillingPlan {
	return []ente.BillingPlan{
		{
			ID:      FreePlanID,
			Storage: FreePlanStorage,
			Price:   "0",
			Period:  ente.PeriodYear,
		},
		{
			ID:      PremiumPlanID,
			Storage: PremiumPlanStorage,
			Price:   "9.99",
			Period:  ente.PeriodYear,
		},
	}
}

// GetFreePlan returns the free plan details
func (c *UPBillingController) GetFreePlan() ente.FreePlan {
	return ente.FreePlan{
		Storage:  FreePlanStorage,
		Period:   ente.PeriodYear,
		Duration: ente.TrialPeriodDuration,
	}
}

// VerifySubscription verifies and returns the verified subscription
func (c *UPBillingController) UPVerifySubscription(
	userID int64) (ente.Subscription, error) {
	// Basic (free) Products ID
	FreePlanProductID := viper.GetString("unplugged.basic-plan-id")
	var newSubscription ente.Subscription

	newUPSubscription, err := c.UPStoreController.GetVerifiedSubscription(userID)
	if err != nil {
		return ente.Subscription{}, stacktrace.Propagate(err, "failed to get verified subscription")
	}

	if newUPSubscription.Type == FreePlanID {
		subscription, errSub := c.BillingRepo.GetUserSubscription(userID)
		if errSub != nil {
			return ente.Subscription{}, stacktrace.Propagate(err, "")
		}
		if subscription.ProductID == ente.FreePlanProductID || subscription.ProductID == FreePlanProductID {
			return subscription, nil
		}
		return ente.Subscription{}, stacktrace.Propagate(ente.ErrCannotDowngrade, "")
	}

	currentSubscription, err := c.BillingRepo.GetUserSubscription(userID)
	if err != nil {
		return ente.Subscription{}, stacktrace.Propagate(err, "")
	}
	convertSubscription(userID, &newSubscription, &newUPSubscription, currentSubscription)

	newSubscriptionExpiresSooner := newSubscription.ExpiryTime < currentSubscription.ExpiryTime
	isUpgradingFromFreePlan := currentSubscription.ProductID == ente.FreePlanProductID
	hasChangedProductID := currentSubscription.ProductID != newSubscription.ProductID
	isOutdatedPurchase := !isUpgradingFromFreePlan && !hasChangedProductID && newSubscriptionExpiresSooner

	if isOutdatedPurchase {
		// User is reporting an outdated purchase that was already verified
		// no-op
		log.Info("Outdated purchase reported")
		return currentSubscription, nil
	}
	if newSubscription.Storage < currentSubscription.Storage {
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
		existingSub, existingSubErr := c.BillingRepo.GetSubscriptionForTransaction(newSubscription.OriginalTransactionID, UnpluggedProvider)
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
		currentSubscription.ID,
		newSubscription,
	)
	if err != nil {
		return ente.Subscription{}, stacktrace.Propagate(err, "")
	}
	log.Info("Replaced subscription")
	newSubscription.ID = currentSubscription.ID

	log.Info("Returning new subscription with ID " + strconv.FormatInt(newSubscription.ID, 10))
	return newSubscription, nil
}

func convertSubscription(userID int64, newSubscription *ente.Subscription, newUPSubscription *UpSubscriptionDetails,
	currentSubscription ente.Subscription) {
	newSubscription.ID = currentSubscription.ID
	newSubscription.Period = newUPSubscription.Interval
	newSubscription.PaymentProvider = UnpluggedProvider
	newSubscription.UserID = userID
	newSubscription.ExpiryTime = newUPSubscription.ExpirationDate.UnixNano() / 1000
	newSubscription.ProductID = newUPSubscription.Price
	newSubscription.Storage = PremiumPlanStorage
	newSubscription.OriginalTransactionID = newUPSubscription.ID
	newSubscription.Price = newUPSubscription.Price
	newSubscription.Attributes.IsCancelled = newUPSubscription.EndReason == "CANCELLED"
}

// GetUserPlans returns the available plans for a user
func (c *UPBillingController) GetUserPlans(ctx context.Context, userID int64) ([]ente.BillingPlan, error) {
	return c.GetPlans(), nil
}

// GetSubscription returns the current subscription for a user if any
func (c *UPBillingController) GetSubscription(ctx context.Context, userID int64, token string) (ente.Subscription, error) {
	subscription, err := c.BillingRepo.GetUserSubscription(userID)
	if err != nil {
		return ente.Subscription{}, stacktrace.Propagate(err, "")
	}

	// If the user doesn't have an Unplugged subscription, return the current subscription
	if subscription.PaymentProvider != UnpluggedProvider {
		return subscription, nil
	}

	// Get the latest subscription details from Unplugged
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	urlSubscriptionInner := viper.GetString("unplugged.inner-api-host")
	req, err := http.NewRequest("GET", urlSubscriptionInner+UnpluggedSubscriptionDetailsEndpoint, nil)
	if err != nil {
		return subscription, stacktrace.Propagate(err, "failed to create request")
	}

	req.Header.Add("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return subscription, stacktrace.Propagate(err, "failed to get subscription details")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return subscription, stacktrace.Propagate(fmt.Errorf("unexpected status code: %d", resp.StatusCode), "")
	}

	var details UpSubscriptionDetails
	if err := json.NewDecoder(resp.Body).Decode(&details); err != nil {
		return subscription, stacktrace.Propagate(err, "failed to decode response")
	}

	// Update subscription based on the response
	newSubscription := subscription

	// Set storage based on subscription type
	if details.Type == "PREMIUM" {
		newSubscription.Storage = PremiumPlanStorage
		newSubscription.ProductID = PremiumPlanID
	} else {
		newSubscription.Storage = FreePlanStorage
		newSubscription.ProductID = FreePlanID
	}

	// Set expiry time based on interval
	var expiryDuration time.Duration
	if details.Interval == "YEAR" {
		expiryDuration = 365 * 24 * time.Hour
	} else {
		expiryDuration = 30 * 24 * time.Hour
	}

	newSubscription.ExpiryTime = time.Now().Add(expiryDuration).UnixNano() / 1000
	newSubscription.PaymentProvider = ente.PaymentProvider(details.Provider)
	newSubscription.OriginalTransactionID = details.ID
	newSubscription.Price = details.Price
	newSubscription.Period = details.Interval

	// Update the subscription in the database
	err = c.BillingRepo.ReplaceSubscription(subscription.ID, newSubscription)
	if err != nil {
		return subscription, stacktrace.Propagate(err, "failed to update subscription")
	}

	return newSubscription, nil
}

// CancelSubscription cancels the user's subscription
func (c *UPBillingController) CancelSubscription(ctx context.Context, userID int64, token string) error {
	subscription, err := c.BillingRepo.GetUserSubscription(userID)
	if err != nil {
		return stacktrace.Propagate(err, "")
	}

	// If the user doesn't have an Unplugged subscription, return an error
	if subscription.PaymentProvider != UnpluggedProvider {
		return stacktrace.Propagate(fmt.Errorf("user does not have an Unplugged subscription"), "")
	}

	// Call Unplugged API to cancel subscription
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	urlSubscriptionInner := viper.GetString("unplugged.inner-api-host")

	req, err := http.NewRequest("DELETE", urlSubscriptionInner+UnpluggedSubscriptionCancelEndpoint, nil)
	if err != nil {
		return stacktrace.Propagate(err, "failed to create request")
	}

	req.Header.Add("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return stacktrace.Propagate(err, "failed to cancel subscription")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return stacktrace.Propagate(fmt.Errorf("unexpected status code: %d", resp.StatusCode), "")
	}

	// Update subscription to free plan
	newSubscription := subscription
	newSubscription.Storage = FreePlanStorage
	newSubscription.ProductID = FreePlanID
	newSubscription.Attributes.IsCancelled = true

	err = c.BillingRepo.ReplaceSubscription(subscription.ID, newSubscription)
	if err != nil {
		return stacktrace.Propagate(err, "failed to update subscription")
	}

	return nil
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
			log.Errorf("failed to verify subscription for user: %s, Err: %s", emailUser.Email, err)
			return stacktrace.Propagate(err, "failed to verify subscription for user")
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
