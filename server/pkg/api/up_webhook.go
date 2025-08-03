package api

import (
	"io"
	"net/http"

	"github.com/ente-io/museum/pkg/controller"
	"github.com/ente-io/museum/pkg/utils/handler"
	"github.com/ente-io/stacktrace"
	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

// UPWebhookHandler handles webhook requests for Unplugged subscriptions
type UPWebhookHandler struct {
	Controller *controller.UPBillingController
}

// HandleSubscriptionWebhook processes subscription webhook notifications
func (h *UPWebhookHandler) HandleSubscriptionWebhook(c *gin.Context) {
	// Read the request body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		handler.Error(c, stacktrace.Propagate(err, "Failed to read request body"))
		return
	}

	// Process the webhook notification
	err = h.Controller.HandleSubscriptionWebhook(c.Request.Context(), body)
	if err != nil {
		log.WithFields(log.Fields{
			"req_id": requestid.Get(c),
			"error":  err.Error(),
		}).Error("Failed to process subscription webhook")
		handler.Error(c, stacktrace.Propagate(err, "Failed to process subscription webhook"))
		return
	}

	log.WithFields(log.Fields{
		"req_id": requestid.Get(c),
	}).Info("Successfully processed subscription webhook")

	// Return a success response
	c.JSON(http.StatusOK, gin.H{})
}
