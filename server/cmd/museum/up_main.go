package main

// Unplugged (UP) wiring. Kept out of main.go so that upstream museum files
// carry only one-line call-outs, each marked with a "UP" comment.

import (
	"time"

	"github.com/ente/museum/pkg/api"
	"github.com/ente/museum/pkg/controller"
	"github.com/ente/museum/pkg/controller/commonbilling"
	"github.com/ente/museum/pkg/controller/discord"
	"github.com/ente/museum/pkg/controller/emergency"
	"github.com/ente/museum/pkg/controller/user"
	"github.com/ente/museum/pkg/middleware"
	"github.com/ente/museum/pkg/repo"
	"github.com/ente/museum/pkg/utils"
	"github.com/ente/museum/pkg/utils/auth"
	"github.com/gin-gonic/gin"
	"github.com/patrickmn/go-cache"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func upSetDefaults() {
	viper.SetDefault("unplugged.api-host", "https://up-app-dev.unpluggedsystems.app")
	viper.SetDefault("unplugged.inner-api-host", "https://up-app-dev.unpluggedsystems.app")
	viper.SetDefault("unplugged.email-host", "msg.unpluggedsystems.app")
	viper.SetDefault("unplugged.basic-plane-id", "free")
}

type upServer struct {
	UserHandler           *api.UPUserHandler
	adminHandler          *api.UPAdminHandler
	webhookHandler        *api.UPWebhookHandler
	accessTokenMiddleware *middleware.UPAccessTokenMiddleware
}

func newUPServer(
	userRepo *repo.UserRepository,
	billingRepo *repo.BillingRepository,
	usageRepo *repo.UsageRepository,
	fileRepo *repo.FileRepository,
	commonBillController *commonbilling.Controller,
	userController *user.UserController,
	emergencyCtrl *emergency.Controller,
	discordController *discord.DiscordController,
	hashingKeyBytes []byte,
) *upServer {
	jwtValidator, err := auth.NewJWTValidator()
	if err != nil {
		log.Fatalf("Failed to initialize JWT validator: %v", err)
	}
	userUtils := &utils.User{UserRepo: userRepo, HashingKey: hashingKeyBytes}
	upStoreController := controller.NewUPStoreController(billingRepo, fileRepo, userRepo, commonBillController)
	upBillingController := controller.NewUPBillingController(
		billingRepo,
		userRepo,
		usageRepo,
		upStoreController,
		userUtils,
		hashingKeyBytes,
	)
	upBillingController.CommonBillCtrl = commonBillController

	return &upServer{
		UserHandler: &api.UPUserHandler{
			UserController:      userController,
			JWTValidator:        jwtValidator,
			UPBillingController: upBillingController,
			UPStoreController:   upStoreController,
			UserUtils:           userUtils,
		},
		adminHandler: &api.UPAdminHandler{
			UserRepo:            userRepo,
			UserController:      userController,
			EmergencyController: emergencyCtrl,
			DiscordController:   discordController,
			UserUtils:           userUtils,
		},
		webhookHandler: &api.UPWebhookHandler{
			UpBillingController: upBillingController,
		},
		accessTokenMiddleware: &middleware.UPAccessTokenMiddleware{
			JWTValidator: jwtValidator,
			Cache:        cache.New(1*time.Minute, 15*time.Minute),
			UserUtils:    userUtils,
		},
	}
}

// registerRoutes registers the UP-only endpoints. Upstream endpoints whose
// behaviour UP overrides are re-pointed at s.UserHandler in main.go instead,
// since gin does not allow registering the same route twice.
func (s *upServer) registerRoutes(
	server *gin.Engine,
	publicAPI *gin.RouterGroup,
	rateLimiter *middleware.RateLimitMiddleware,
	authMiddleware middleware.AuthMiddleware,
) {
	upPrivateAPI := server.Group("/")
	upPrivateAPI.Use(s.accessTokenMiddleware.UPAccessTokenAuthMiddleware(), rateLimiter.APIRateLimitForUserMiddleware(urlSanitizer))
	upPrivateAPI.POST("/users/up/ott", s.UserHandler.SendOTT)

	upAdminAPI := server.Group("/up-admin")
	upAdminAPI.Use(s.accessTokenMiddleware.UPAccessTokenAuthMiddleware(), authMiddleware.AdminAuthMiddleware())
	upAdminAPI.DELETE("/user/delete", s.adminHandler.UPDeleteUser)
	upAdminAPI.GET("/users", s.adminHandler.UPGetUsers)

	webhookMiddleware := &middleware.WebhookSignatureMiddleware{
		Secret: viper.GetString("unplugged.webhook-secret"),
	}
	publicAPI.POST("/webhooks/up/subscriptions", webhookMiddleware.VerifySignature(), s.webhookHandler.HandleSubscriptionWebhook)
}
