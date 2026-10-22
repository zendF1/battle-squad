package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/lobby"
	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/game/room"
	"battle-squad/internal/game/ws"
	"battle-squad/internal/gamedatasync"
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
	// "__"-prefixed events are server-internal actor messages; never accept them from clients
	if strings.HasPrefix(msg.Event, "__") {
		log := observability.FromContext(ctx)
		log.Warn().Str("event", msg.Event).Str("playerId", client.PlayerID).Msg("rejected internal event from client")
		return
	}
	// Try lobby handler first
	if c.lobbyHandler.HandleLobbyMessage(ctx, client, msg) {
		return
	}
	// Fall through to room handler
	c.roomHandler.HandleMessage(ctx, client, msg)
}

// Register marks the connection's player online (the invite list, see the
// lobby presence); the newest connection of a player wins.
func (c *CompositeWSHandler) Register(client *ws.Client) {
	c.lobbyHandler.RegisterOnline(client)
}

func (c *CompositeWSHandler) Unregister(client *ws.Client) {
	c.lobbyHandler.UnregisterFromLobby(client)
	c.roomHandler.Unregister(client)
}

// matchMaps are the snapshot's map ids and their minimum rank tiers (bronze when unset).
func matchMaps(s *gamedata.Snapshot) ([]string, map[string]string) {
	ids := make([]string, 0, len(s.Data.Maps))
	tiers := make(map[string]string, len(s.Data.Maps))
	for mapID, mapCfg := range s.Data.Maps {
		if s.IsBossArena(mapID) {
			continue // boss arenas are not in the PvP rotation
		}
		ids = append(ids, mapID)
		tier := mapCfg.MinRankTier
		if tier == "" {
			tier = "bronze"
		}
		tiers[mapID] = tier
	}
	sort.Strings(ids)
	return ids, tiers
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

	// Generate unique node ID for clustering / redis tracking
	nodeID := os.Getenv("NODE_ID")
	if nodeID == "" {
		nodeID = "node-game-1"
	}

	// 4. Game data: the DB (YAML files as the fallback), reloaded when an admin saves (Redis
	// gamedata:changed, or the revision poll). Every match keeps the snapshot it started with.
	reloadCtx, stopReload := context.WithCancel(context.Background())
	defer stopReload()
	reloader := gamedatasync.New(db, redisClient, "game", nodeID, "configs")
	if err := reloader.Start(reloadCtx); err != nil {
		log.Fatal().Err(err).Msg("failed to load game data")
	}
	log.Info().Str("version", gamedata.Current().Version).Msg("game configurations loaded successfully")

	// 5. Init JWT Access Manager
	jwtAccess := sharedAuth.NewJWTManager(cfg.JWTSecret, 15*time.Minute)

	// 6. Init WS Handlers and Hub

	// Room hub
	roomHub := room.NewHub(redisClient, db, nodeID)
	roomHub.SetDevUnlockAll(cfg.DevUnlockAll)
	if cfg.DevUnlockAll {
		log.Warn().Msg("DEV_UNLOCK_ALL enabled: QuickPlay skips character/item ownership and item consumption")
	}
	roomWSHandler := room.NewWSHandler(roomHub)

	// Lobby hub
	lobbyHub := lobby.NewLobbyHub(db, redisClient, nodeID)

	// Matchmaker, with the available map IDs and their tier restrictions (refreshed on reload)
	mapIDs, mapTiers := matchMaps(gamedata.Current())
	mm := matchmaker.NewMatchmaker(db, redisClient, nodeID, roomHub, mapIDs, mapTiers)
	reloader.OnPublish(func(s *gamedata.Snapshot) { mm.SetMaps(matchMaps(s)) })
	go mm.Run()

	// Lobby WS handler
	lobbyWSHandler := lobby.NewWSHandler(lobbyHub, mm)

	// Matchmade games notify the players' lobbies: MatchFound (the lobby clients
	// join the battle room before the match goroutine starts) and ReturnToLobby
	// when the match is over.
	roomHub.SetLobbyNotifier(func(lobbyID string, event string, data interface{}, battleRoom *room.Room) {
		var join func(c *ws.Client)
		if event == "MatchFound" && battleRoom != nil {
			join = battleRoom.RegisterClient
		}
		if err := lobbyHub.Notify(lobbyID, event, data, join); err != nil {
			observability.Log.Warn().Err(err).Str("lobbyId", lobbyID).Str("event", event).Msg("lobby notify failed")
		}
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
