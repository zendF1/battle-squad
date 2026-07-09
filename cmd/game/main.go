package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/lobby"
	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/game/room"
	"battle-squad/internal/game/ws"
	sharedAuth "battle-squad/internal/shared/auth"
	"battle-squad/internal/shared/config"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// CompositeWSHandler delegates WebSocket messages to the lobby handler first,
// then falls through to the room handler if the event is not lobby-related.
type CompositeWSHandler struct {
	lobbyHandler *lobby.WSHandler
	roomHandler  *room.WSHandler
}

func (c *CompositeWSHandler) HandleMessage(ctx context.Context, client *ws.Client, msg ws.Message) {
	// Try lobby handler first
	if c.lobbyHandler.HandleLobbyMessage(ctx, client, msg) {
		return
	}
	// Fall through to room handler
	c.roomHandler.HandleMessage(ctx, client, msg)
}

func (c *CompositeWSHandler) Unregister(client *ws.Client) {
	c.lobbyHandler.UnregisterFromLobby(client)
	c.roomHandler.Unregister(client)
}

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

	// Room hub
	roomHub := room.NewHub(redisClient, db, nodeID)
	roomWSHandler := room.NewWSHandler(roomHub)

	// Lobby hub
	lobbyHub := lobby.NewLobbyHub(db, redisClient, nodeID)

	// Collect available map IDs and their tier restrictions
	mapIDs := make([]string, 0)
	mapTiers := make(map[string]string)
	for mapID, mapCfg := range gamedata.Data.Maps {
		mapIDs = append(mapIDs, mapID)
		tier := mapCfg.MinRankTier
		if tier == "" {
			tier = "bronze"
		}
		mapTiers[mapID] = tier
	}

	// Matchmaker
	mm := matchmaker.NewMatchmaker(db, redisClient, nodeID, roomHub, mapIDs, mapTiers)
	go mm.Run()

	// Lobby WS handler
	lobbyWSHandler := lobby.NewWSHandler(lobbyHub, mm)

	// Wire lobby notifier so room hub can notify lobby clients on MatchFound.
	// The battleRoom is passed directly to avoid deadlock (CreateBattleFromMatch
	// holds hub lock, so we cannot call FindRoom which also needs the lock).
	roomHub.SetLobbyNotifier(func(lobbyID string, event string, data interface{}, battleRoom *room.Room) {
		observability.Log.Info().
			Str("lobbyId", lobbyID).
			Str("event", event).
			Msg("[RANKED-DEBUG] lobbyNotifier called")

		l, err := lobbyHub.FindLobby(lobbyID)
		if err != nil {
			observability.Log.Error().Err(err).
				Str("lobbyId", lobbyID).
				Msg("[RANKED-DEBUG] lobbyNotifier: FindLobby FAILED")
			return
		}

		observability.Log.Info().
			Str("lobbyId", lobbyID).
			Int("lobbyClientCount", len(l.Clients)).
			Msg("[RANKED-DEBUG] lobbyNotifier: found lobby")

		payload, _ := json.Marshal(data)
		for _, c := range l.Clients {
			observability.Log.Info().
				Str("lobbyId", lobbyID).
				Str("playerId", c.PlayerID).
				Str("event", event).
				Msg("[RANKED-DEBUG] lobbyNotifier: sending event to lobby client")
			select {
			case c.Send <- ws.Message{Event: event, Data: payload}:
				observability.Log.Info().
					Str("playerId", c.PlayerID).
					Msg("[RANKED-DEBUG] lobbyNotifier: event sent OK")
			default:
				observability.Log.Warn().
					Str("playerId", c.PlayerID).
					Msg("[RANKED-DEBUG] lobbyNotifier: Send channel FULL, event DROPPED")
			}
		}

		// For MatchFound, register lobby clients directly in the battle room
		// so they receive MatchStarted and subsequent match events.
		if event == "MatchFound" && battleRoom != nil {
			for _, c := range l.Clients {
				observability.Log.Info().
					Str("roomId", battleRoom.ID).
					Str("playerId", c.PlayerID).
					Msg("[RANKED-DEBUG] lobbyNotifier: calling RegisterClient")
				battleRoom.RegisterClient(c)
			}
		}

		l.SetStatus("in_match")
		observability.Log.Info().
			Str("lobbyId", lobbyID).
			Msg("[RANKED-DEBUG] lobbyNotifier: done, lobby set to in_match")
	})

	// Composite handler
	compositeHandler := &CompositeWSHandler{
		lobbyHandler: lobbyWSHandler,
		roomHandler:  roomWSHandler,
	}

	wsServer := ws.NewServer(jwtAccess, db, redisClient, compositeHandler, cfg)

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

	mm.Stop()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Game Server forced to shutdown")
	}

	log.Info().Msg("Game Server stopped")
}
