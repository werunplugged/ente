package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newWebhookRouter(secret string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	m := &WebhookSignatureMiddleware{Secret: secret}
	r.POST("/hook", m.VerifySignature(), func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func postHook(r *gin.Engine, body string, signature string) int {
	req := httptest.NewRequest(http.MethodPost, "/hook", strings.NewReader(body))
	if signature != "" {
		req.Header.Set("X-Webhook-Signature", signature)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestUPWebhookSignature(t *testing.T) {
	body := `{"username":"alice","event":"updated"}`
	valid, err := generateSignature(body, "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	forged, _ := generateSignature(body, "")

	cases := []struct {
		name      string
		secret    string
		signature string
		want      int
	}{
		{"valid", "s3cret", valid, http.StatusOK},
		{"wrong signature", "s3cret", forged, http.StatusUnauthorized},
		{"missing signature", "s3cret", "", http.StatusUnauthorized},
		{"secret not configured", "", forged, http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := postHook(newWebhookRouter(tc.secret), body, tc.signature); got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}
