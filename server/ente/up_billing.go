package ente

import "time"

const (
	Unplugged PaymentProvider = "unplugged"
	// Storage sizes in bytes
	PremiumPlanStorage int64 = 1000 * 1024 * 1024 * 1024 // 1TB

	// Plan IDs
	FreePlanID    = "BASIC"
	PremiumPlanID = "PREMIUM"
)

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
type WebhookRequest struct {
	ID              string  `json:"id"`
	Type            string  `json:"type"`
	Interval        string  `json:"interval"`
	Price           float64 `json:"price"`
	Currency        string  `json:"currency"`
	Provider        string  `json:"provider"`
	NextChargeDate  int64   `json:"nextChargeDate"`
	ExpirationDate  int64   `json:"expirationDate"`
	ServerTimestamp int64   `json:"serverTimestamp"`
	Username        string  `json:"username"`
	Event           string  `json:"event"`
}
