package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"battle-squad/internal/shared/config"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
	"battle-squad/internal/worker"
	"battle-squad/internal/worker/jobs"
)

func main() {
	// 1. Load config
	cfg := config.LoadConfig()

	// 2. Init logger
	observability.InitLogger(cfg.Env)
	log := observability.Log
	log.Info().Msg("starting Worker Server...")

	// 3. Connect to Postgres and Redis
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := database.NewPostgresDB(ctx, cfg.PostgresDSN, cfg.DBMaxConns, cfg.DBMinConns)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to Postgres")
	}
	defer db.Close()

	redisClient, err := database.NewRedisClient(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB, cfg.RedisPoolSize, cfg.RedisMinIdle)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to Redis")
	}
	defer redisClient.Close()

	// 4. Build scheduler and register jobs
	scheduler := worker.NewScheduler(db, redisClient)

	scheduler.Register(jobs.NewBanExpireJob(db))
	scheduler.Register(jobs.NewDailyResetJob(db, redisClient))
	scheduler.Register(jobs.NewAccountDeletionJob(db))

	// 5. Start scheduler
	bgCtx := context.Background()
	scheduler.Start(bgCtx)
	log.Info().Msg("Worker scheduler started with 3 jobs")

	// 6. Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down Worker Server gracefully...")
	scheduler.Stop()
	log.Info().Msg("Worker Server stopped")
}
