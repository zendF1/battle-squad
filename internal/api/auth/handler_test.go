package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"battle-squad/internal/shared/auth"
	"battle-squad/internal/shared/database"
)

// newRefreshHandler wires a Service whose Redis is unreachable, so any lookup fails with a
// connection error (not redis.Nil).
func newRefreshHandler() (*Handler, *auth.JWTManager) {
	jwtRefresh := auth.NewJWTManager("test-refresh-secret", time.Hour)
	deadRedis := &database.RedisClient{Client: redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 50 * time.Millisecond,
		MaxRetries:  -1,
	})}
	svc := NewService(nil, deadRedis, auth.NewJWTManager("test-access-secret", time.Hour), jwtRefresh)
	return NewHandler(svc), jwtRefresh
}

func postRefresh(h *Handler, token string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(RefreshTokenRequest{RefreshToken: token})
	rec := httptest.NewRecorder()
	h.RefreshToken(rec, httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader(body)))
	return rec
}

func TestRefreshRejectsMalformedTokenWith401(t *testing.T) {
	h, _ := newRefreshHandler()
	if rec := postRefresh(h, "not-a-jwt"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestRefreshReportsSessionStoreFailureAs500(t *testing.T) {
	h, jwtRefresh := newRefreshHandler()
	token, err := jwtRefresh.Generate("acc-1", "player-1", "player")
	if err != nil {
		t.Fatal(err)
	}
	if rec := postRefresh(h, token); rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (Redis down is not an invalid token)", rec.Code)
	}
}
