package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"battle-squad/internal/api/auth"
	"battle-squad/internal/api/player"
	"battle-squad/internal/api/economy"
	"battle-squad/internal/api/inventory"
	"battle-squad/internal/api/shop"
	"battle-squad/internal/api/iap"
	"battle-squad/internal/api/giftcode"
	"battle-squad/internal/api/mission"
	"battle-squad/internal/api/rank"
	"battle-squad/internal/api/moderation"
	"battle-squad/internal/api/appconfig"
	"battle-squad/internal/shared/idempotency"
	"battle-squad/internal/shared/config"
	"battle-squad/internal/shared/database"
	sharedAuth "battle-squad/internal/shared/auth"
	"battle-squad/internal/shared/middleware"
	"battle-squad/internal/shared/observability"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"golang.org/x/time/rate"
)

func main() {
	// 1. Load config
	cfg := config.LoadConfig()

	// 2. Init Logger
	observability.InitLogger(cfg.Env)
	log := observability.Log
	log.Info().Msg("starting API Server...")

	// 3. Init DB & Redis connections
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := database.NewPostgresDB(ctx, cfg.PostgresDSN)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to Postgres")
	}
	defer db.Close()

	redisClient, err := database.NewRedisClient(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to Redis")
	}
	defer redisClient.Close()

	// 4. Init JWT Managers
	jwtAccess := sharedAuth.NewJWTManager(cfg.JWTSecret, 15*time.Minute)
	jwtRefresh := sharedAuth.NewJWTManager(cfg.JWTSecret, 30*24*time.Hour)

	// 5. Init Modules
	economyRepo := economy.NewRepository()
	_ = economyRepo

	inventoryRepo := inventory.NewRepository(db)
	inventoryService := inventory.NewService(inventoryRepo)
	inventoryHandler := inventory.NewHandler(inventoryService)

	idempotencyManager := idempotency.NewManager(redisClient)

	shopRepo := shop.NewRepository(db)
	shopService := shop.NewService(shopRepo, economyRepo, inventoryRepo, db, idempotencyManager)
	shopHandler := shop.NewHandler(shopService)

	iapRepo := iap.NewRepository(db)
	iapService := iap.NewService(iapRepo, economyRepo, db)
	iapHandler := iap.NewHandler(iapService)

	giftcodeRepo := giftcode.NewRepository(db)
	giftcodeService := giftcode.NewService(giftcodeRepo, economyRepo, inventoryRepo, db)
	giftcodeHandler := giftcode.NewHandler(giftcodeService)

	missionRepo := mission.NewRepository(db)
	missionService := mission.NewService(missionRepo, economyRepo, db)
	missionHandler := mission.NewHandler(missionService)

	rankRepo := rank.NewRepository(db)
	rankService := rank.NewService(rankRepo, economyRepo, db)
	rankHandler := rank.NewHandler(rankService)

	moderationRepo := moderation.NewRepository(db)
	moderationService := moderation.NewService(moderationRepo, db)
	moderationHandler := moderation.NewHandler(moderationService)

	authRepo := auth.NewRepository(db)
	authService := auth.NewService(authRepo, redisClient, jwtAccess, jwtRefresh)
	authHandler := auth.NewHandler(authService)

	playerRepo := player.NewRepository(db)
	playerService := player.NewService(playerRepo)
	playerHandler := player.NewHandler(playerService)

	appconfigService := appconfig.NewService(db, cfg)
	appconfigHandler := appconfig.NewHandler(appconfigService)

	healthHandler := observability.NewHealthHandler(db, redisClient)

	// 6. Router Setup
	r := chi.NewRouter()

	// 7. Middlewares
	r.Use(chiMiddleware.RealIP)
	r.Use(middleware.CorrelationID)
	r.Use(chiMiddleware.Recoverer)
	r.Use(middleware.VersionCheck(cfg))

	// Rate limiter: 10 req/sec rate, burst of 20
	limiter := middleware.NewRateLimiter(rate.Limit(10), 20)
	r.Use(limiter.Limit)

	// 8. Public Endpoints
	r.HandleFunc("/healthz", healthHandler.Healthz)
	r.HandleFunc("/readyz", healthHandler.Readyz)
	r.HandleFunc("/livez", healthHandler.Livez)

	r.Post("/auth/guest-login", authHandler.GuestLogin)
	r.Post("/auth/provider-login", authHandler.ProviderLogin)
	r.Post("/auth/refresh", authHandler.RefreshToken)
	r.Post("/auth/logout", authHandler.Logout)

	r.Get("/app/version-policy", appconfigHandler.GetVersionPolicy)
	r.Get("/app/config", appconfigHandler.GetRemoteConfig)

	// 9. Protected Endpoints (Auth Required)
	r.Group(func(r chi.Router) {
		r.Use(middleware.AuthMiddleware(jwtAccess))

		r.Post("/auth/link-provider", authHandler.LinkProvider)

		r.Get("/player/profile", playerHandler.GetProfile)
		r.Put("/player/profile", playerHandler.UpdateProfile)
		r.Post("/account/deletion/request", playerHandler.RequestAccountDeletion)
		r.Post("/account/deletion/cancel", playerHandler.CancelAccountDeletion)
		r.Get("/account/deletion/status", playerHandler.GetAccountDeletionStatus)

		r.Get("/player/inventory", inventoryHandler.GetInventory)

		r.Get("/shop/offers", shopHandler.GetOffers)
		r.Post("/shop/purchase", shopHandler.Purchase)
		r.Get("/shop/purchases", shopHandler.GetPurchases)

		r.Get("/iap/products", iapHandler.GetProducts)
		r.Post("/iap/verify", iapHandler.VerifyReceipt)

		r.Post("/giftcode/redeem", giftcodeHandler.Redeem)

		r.Get("/missions/daily", missionHandler.GetDailyMissions)
		r.Get("/missions/achievements", missionHandler.GetAchievements)
		r.Post("/missions/claim", missionHandler.ClaimReward)

		r.Get("/rank/me", rankHandler.GetRankMe)
		r.Get("/rank/leaderboard", rankHandler.GetLeaderboard)
		r.Get("/rank/seasons/current", rankHandler.GetCurrentSeason)
		r.Post("/rank/reward/claim", rankHandler.ClaimReward)

		r.Post("/report/player", moderationHandler.CreateReport)
		r.Post("/moderation/ban", moderationHandler.BanPlayer)
		r.Post("/moderation/ban/revoke", moderationHandler.RevokeBan)
	})

	// 10. Start Server
	srv := &http.Server{
		Addr:         ":" + cfg.APIPort,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Info().Str("port", cfg.APIPort).Msg("API Server listening")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("API Server failed to start")
		}
	}()

	// 11. Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down API Server gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("API Server forced to shutdown")
	}

	log.Info().Msg("API Server stopped")
}
