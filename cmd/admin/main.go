package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"battle-squad/internal/admin"
	"battle-squad/internal/shared/config"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
)

func main() {
	// 1. Load config
	cfg := config.LoadConfig()

	// 2. Init Logger
	observability.InitLogger(cfg.Env)
	log := observability.Log
	log.Info().Msg("starting Admin Dashboard...")

	// 3. Init DB & Redis connections
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := database.NewPostgresDB(ctx, cfg.PostgresDSN, cfg.DBMaxConns, cfg.DBMinConns)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to Postgres")
	}
	defer db.Close()

	redis, err := database.NewRedisClient(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB, cfg.RedisPoolSize, cfg.RedisMinIdle)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to Redis")
	}
	defer redis.Close()

	// 4. Seed config on startup (idempotent)
	if err := admin.SeedAll(context.Background(), db, "configs"); err != nil {
		log.Warn().Err(err).Msg("seed config on startup failed (non-fatal)")
	}

	// 5. Determine port
	port := os.Getenv("ADMIN_PORT")
	if port == "" {
		port = "9000"
	}

	// 6. Create server
	srv := admin.NewServer(db, redis, "configs")

	httpServer := &http.Server{
		Addr:         ":" + port,
		Handler:      srv.Routes(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// 7. Start server
	go func() {
		log.Info().Str("port", port).Msg("Admin Dashboard listening")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("admin server failed")
		}
	}()

	// 8. Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down Admin Dashboard gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("admin server forced to shutdown")
	}

	log.Info().Msg("Admin Dashboard stopped")
}
