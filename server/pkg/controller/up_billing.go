package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ente-io/museum/ente"
	"github.com/ente-io/museum/pkg/repo"
	"github.com/ente-io/stacktrace"
)

const (
	// Storage sizes in bytes
	FreePlanStorage    int64 = 10 * 1024 * 1024 * 1024 // 10GB
	PremiumPlanStorage int64 = 50 * 1024 * 1024 * 1024 // 50GB

	// Plan IDs
	FreePlanID    = "up_free"
	PremiumPlanID = "up_premium"

	// Payment provider
	UnpluggedProvider ente.PaymentProvider = "unplugged"

	// Unplugged API endpoints
	UnpluggedSubscriptionDetailsEndpoint = "/api/subscriptions/v2/details"
	UnpluggedSubscriptionCancelEndpoint  = "/api/subscriptions/v2/cancel"
)

// UpSubscriptionDetails represents the response from Unplugged subscription details endpoint
type UpSubscriptionDetails struct {
	ID       string  `json:"id"`
	Type     string  `json:"type"`     // enum BASIC, PREMIUM
	Interval string  `json:"interval"` // YEAR, MONTH
	Price    float64 `json:"price"`
	Currency string  `json:"currency"`
	Provider string  `json:"provider"` // UNPLUGGED, STRIPE
}

// UPBillingController provides abstractions for handling Unplugged billing related queries
type UPBillingController struct {
	BillingRepo *repo.BillingRepository
	UserRepo    *repo.UserRepository
	UsageRepo   *repo.UsageRepository
	APIHost     string
}

// NewUPBillingController returns a new instance of UPBillingController
func NewUPBillingController(
	billingRepo *repo.BillingRepository,
	userRepo *repo.UserRepository,
	usageRepo *repo.UsageRepository,
	apiHost string,
) *UPBillingController {
	return &UPBillingController{
		BillingRepo: billingRepo,
		UserRepo:    userRepo,
		UsageRepo:   usageRepo,
		APIHost:     apiHost,
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

	req, err := http.NewRequest("GET", c.APIHost+UnpluggedSubscriptionDetailsEndpoint, nil)
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
	newSubscription.PaymentProvider = UnpluggedProvider
	newSubscription.OriginalTransactionID = details.ID
	newSubscription.Price = fmt.Sprintf("%.2f", details.Price)
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

	req, err := http.NewRequest("DELETE", c.APIHost+UnpluggedSubscriptionCancelEndpoint, nil)
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

// GetUsage returns the storage usage for the requesting user
func (c *UPBillingController) GetUsage(ctx context.Context, userID int64) (int64, error) {
	usage, err := c.UsageRepo.GetUsage(userID)
	if err != nil {
		return 0, stacktrace.Propagate(err, "")
	}
	return usage, nil
}
