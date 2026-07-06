package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/room"
	"battle-squad/internal/game/ws"
	sharedAuth "battle-squad/internal/shared/auth"
	"battle-squad/internal/shared/config"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// 1. Load config
	cfg := config.LoadConfig()

	// 2. Init Logger
	observability.InitLogger(cfg.Env)
	log := observability.Log
	log.Info().Msg("starting Game Server...")

	// 3. Init DB & Redis connections (moved before game data loading for DB config support)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := database.NewPostgresDB(ctx, cfg.PostgresDSN)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to Postgres")
	}
	defer db.Close()

	// 4. Load static game data configurations — try DB first, fall back to YAML
	if err := gamedata.LoadGameDataFromDB(db); err != nil {
		log.Warn().Err(err).Msg("failed to load config from DB, falling back to YAML")
		if err := gamedata.LoadGameData("configs"); err != nil {
			log.Fatal().Err(err).Msg("failed to load game data")
		}
	}
	log.Info().Msg("game configurations loaded successfully")

	redisClient, err := database.NewRedisClient(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to Redis")
	}
	defer redisClient.Close()

	// 5. Init JWT Access Manager
	jwtAccess := sharedAuth.NewJWTManager(cfg.JWTSecret, 15*time.Minute)

	// 6. Init WS Handlers and Hub
	// Generate unique node ID for clustering / redis tracking
	nodeID := os.Getenv("NODE_ID")
	if nodeID == "" {
		nodeID = "node-game-1"
	}

	roomHub := room.NewHub(redisClient, db, nodeID)
	roomWSHandler := room.NewWSHandler(roomHub)

	wsServer := ws.NewServer(jwtAccess, db, redisClient, roomWSHandler, cfg)

	// 7. Route Upgrade Handler
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wsServer.HandleUpgrade)

	// Expose operational metrics/health checks on game server as well
	healthHandler := observability.NewHealthHandler(db, redisClient)
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", healthHandler.Healthz)
	mux.HandleFunc("/readyz", healthHandler.Readyz)
	mux.HandleFunc("/livez", healthHandler.Livez)

	srv := &http.Server{
		Addr:         ":" + cfg.GamePort,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Info().Str("port", cfg.GamePort).Msg("Game Server listening (WebSocket)")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Game Server failed to start")
		}
	}()

	// 8. Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down Game Server gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Game Server forced to shutdown")
	}

	log.Info().Msg("Game Server stopped")
}
