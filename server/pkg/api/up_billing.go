package api

import (
	"github.com/ente-io/museum/ente"
	"net/http"

	"github.com/ente-io/museum/pkg/controller"
	"github.com/ente-io/museum/pkg/utils/auth"
	"github.com/ente-io/museum/pkg/utils/handler"
	"github.com/ente-io/stacktrace"
	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

// UPBillingHandler exposes request handlers for all Unplugged billing related requests
type UPBillingHandler struct {
	Controller *controller.UPBillingController
}

// GetPlansV2 returns the available Unplugged subscription plans
func (h *UPBillingHandler) GetPlansV2(c *gin.Context) {
	plans := h.Controller.GetPlans()
	freePlan := h.Controller.GetFreePlan()

	log.Info(log.Fields{
		"req_id":   requestid.Get(c),
		"plans":    plans,
		"freePlan": freePlan,
	})

	c.JSON(http.StatusOK, gin.H{
		"plans":    plans,
		"freePlan": freePlan,
	})
}

// GetUserPlans returns the available plans for the user
func (h *UPBillingHandler) GetUserPlans(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Header)
	plans, err := h.Controller.GetUserPlans(c.Request.Context(), userID)
	if err != nil {
		handler.Error(c, stacktrace.Propagate(err, "Failed to get user plans"))
		return
	}
	freePlan := h.Controller.GetFreePlan()

	log.Info(log.Fields{
		"user_id":  userID,
		"req_id":   requestid.Get(c),
		"plans":    plans,
		"freePlan": freePlan,
	})

	c.JSON(http.StatusOK, gin.H{
		"plans":    plans,
		"freePlan": freePlan,
	})
}

// VerifySubscription verifies and returns the verified subscription
func (h *UPBillingHandler) VerifySubscription(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Header)
	var request ente.SubscriptionVerificationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		handler.Error(c, stacktrace.Propagate(err, ""))
		return
	}
	subscription, err := h.Controller.UPVerifySubscription(userID)
	if err != nil {
		handler.Error(c, stacktrace.Propagate(err, ""))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"subscription": subscription,
	})
}

// GetSubscription returns the current subscription for a user if any
func (h *UPBillingHandler) GetSubscription(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Header)
	token := c.GetHeader("Authorization")

	// Extract the token from the Authorization header (Bearer token)
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	}

	subscription, err := h.Controller.GetSubscription(c.Request.Context(), userID, token)
	if err != nil {
		handler.Error(c, stacktrace.Propagate(err, "Failed to get subscription"))
		return
	}

	log.Info(log.Fields{
		"user_id":      userID,
		"req_id":       requestid.Get(c),
		"subscription": subscription,
	})

	c.JSON(http.StatusOK, gin.H{
		"subscription": subscription,
	})
}

// CancelSubscription cancels the user's subscription
func (h *UPBillingHandler) CancelSubscription(c *gin.Context) {
	userID := auth.GetUserID(c.Request.Header)
	token := c.GetHeader("Authorization")

	// Extract the token from the Authorization header (Bearer token)
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	}

	err := h.Controller.CancelSubscription(c.Request.Context(), userID, token)
	if err != nil {
		handler.Error(c, stacktrace.Propagate(err, "Failed to cancel subscription"))
		return
	}

	log.Info(log.Fields{
		"user_id": userID,
		"req_id":  requestid.Get(c),
		"message": "Subscription cancelled successfully",
	})

	c.JSON(http.StatusOK, gin.H{
		"message": "Subscription cancelled successfully",
	})
}
