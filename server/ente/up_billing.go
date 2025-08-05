package ente

const (
	Unplugged PaymentProvider = "unplugged"
)

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
