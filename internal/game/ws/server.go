package ws

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"sync"

	"battle-squad/internal/shared/auth"
	"battle-squad/internal/shared/config"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/model"
	"battle-squad/internal/shared/observability"

	"github.com/gorilla/websocket"
)

const (
	maxConnsPerIP = 5
)

type Server struct {
	upgrader    websocket.Upgrader
	jwtManager  *auth.JWTManager
	db          *database.PostgresDB
	redis       *database.RedisClient
	handler     HandlerInterface
	cfg         *config.Config
	ipConns     map[string]int
	ipConnsMu   sync.Mutex
}

func NewServer(
	jwtManager *auth.JWTManager,
	db *database.PostgresDB,
	redis *database.RedisClient,
	handler HandlerInterface,
	cfg *config.Config,
) *Server {
	return &Server{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				// Adjust origin check for production as necessary
				return true
			},
		},
		jwtManager: jwtManager,
		db:         db,
		redis:      redis,
		handler:    handler,
		cfg:        cfg,
		ipConns:    make(map[string]int),
	}
}

func (s *Server) HandleUpgrade(w http.ResponseWriter, r *http.Request) {
	log := observability.Log

	// 1. Version checking
	protocolStr := r.URL.Query().Get("protocolVersion")
	if protocolStr == "" {
		protocolStr = r.Header.Get("X-Protocol-Version")
	}
	if protocolStr != "" {
		proto, err := strconv.Atoi(protocolStr)
		if err != nil || proto < s.cfg.ProtocolVersion {
			model.WriteError(w, r, model.ErrForceUpdate)
			return
		}
	}

	// 2. Token authentication
	tokenStr := r.URL.Query().Get("token")
	if tokenStr == "" {
		// Fallback to Header if query param is empty
		authHeader := r.Header.Get("Authorization")
		if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
			tokenStr = authHeader[7:]
		}
	}

	if tokenStr == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	claims, err := s.jwtManager.Verify(tokenStr)
	if err != nil {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	// 3. Ban check
	isBanned, err := s.checkAccountBan(r.Context(), claims.AccountID)
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	if isBanned {
		model.WriteError(w, r, model.ErrBanned)
		return
	}

	// 4. IP connection limit
	clientIP := extractIP(r)
	s.ipConnsMu.Lock()
	if s.ipConns[clientIP] >= maxConnsPerIP {
		s.ipConnsMu.Unlock()
		log.Warn().Str("ip", clientIP).Int("count", s.ipConns[clientIP]).Msg("IP connection limit reached")
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return
	}
	s.ipConns[clientIP]++
	s.ipConnsMu.Unlock()

	// 5. Upgrade connection
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("failed to upgrade websocket connection")
		s.ipConnsMu.Lock()
		s.ipConns[clientIP]--
		if s.ipConns[clientIP] <= 0 {
			delete(s.ipConns, clientIP)
		}
		s.ipConnsMu.Unlock()
		return
	}

	observability.WSConnectionsTotal.Inc()
	observability.ActiveConnections.Inc()

	client := &Client{
		Conn:          conn,
		Send:          make(chan Message, 256),
		PlayerID:      claims.PlayerID,
		AccountID:     claims.AccountID,
		WSHandHandler: s.handler,
		server:        s,
		ip:            clientIP,
	}

	go client.WritePump()
	go client.ReadPump()

	log.Info().Str("playerId", client.PlayerID).Str("ip", clientIP).Msg("websocket client connected")
}

func extractIP(r *http.Request) string {
	// Check X-Forwarded-For for proxied connections
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// First IP in the list is the client
		if idx := len(xff); idx > 0 {
			for i, ch := range xff {
				if ch == ',' {
					return xff[:i]
				}
			}
			return xff
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// DecrementIPConn is called when a client disconnects to release the IP slot.
func (s *Server) DecrementIPConn(ip string) {
	s.ipConnsMu.Lock()
	s.ipConns[ip]--
	if s.ipConns[ip] <= 0 {
		delete(s.ipConns, ip)
	}
	s.ipConnsMu.Unlock()
}

func (s *Server) checkAccountBan(ctx context.Context, accountID string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM account_bans
			WHERE account_id = $1 AND status = 'active' AND (ends_at IS NULL OR ends_at > CURRENT_TIMESTAMP)
		)
	`
	var isBanned bool
	err := s.db.Pool.QueryRow(ctx, query, accountID).Scan(&isBanned)
	if err != nil {
		return false, err
	}
	return isBanned, nil
}
