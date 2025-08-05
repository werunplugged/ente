package ente

import "time"

const (
	Unplugged PaymentProvider = "unplugged"
)

type WebhookRequest struct {
	ID              string    `json:"id"`
	Type            string    `json:"type"`
	Interval        string    `json:"interval"`
	Price           float64   `json:"price"`
	Currency        string    `json:"currency"`
	Provider        string    `json:"provider"`
	NextChargeDate  time.Time `json:"nextChargeDate"`
	ExpirationDate  time.Time `json:"expirationDate"`
	ServerTimestamp time.Time `json:"serverTimestamp"`
	Username        string    `json:"username"`
	Event           string    `json:"event"`
}
