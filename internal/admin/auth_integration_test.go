package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/migrate"
)

// sessionRedis is Redis DB 7 at TEST_REDIS_ADDR or localhost:6379 (skipped without one).
func sessionRedis(t *testing.T) *database.RedisClient {
	t.Helper()
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	r, err := database.NewRedisClient(addr, "", 7, 4, 0)
	if err != nil {
		t.Skipf("redis: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func send(h http.Handler, method, path string, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// Outside production the dashboard stays open until the first admin exists; then every page
// needs a sign-in with the bootstrapped account, and signing out ends the session. In
// production it needs one from the start.
func TestAdminLogin(t *testing.T) {
	db := seededDB(t)
	ctx := context.Background()
	s := NewServer(db, sessionRedis(t), configsDir())
	s.notifyChannel = "gamedata:changed:test:" + t.Name()
	h := s.Routes()

	if rec := send(h, "GET", "/physics", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("dev without an admin: %d", rec.Code)
	}
	if res, err := migrate.BootstrapAdmin(ctx, db.Pool, "Ops@Example.com", "a-long-enough-secret"); err != nil || res != migrate.AdminCreated {
		t.Fatalf("bootstrap: %v %v", res, err)
	}
	if rec := send(h, "GET", "/physics", nil, nil); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("no session: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if rec := send(h, "POST", "/physics/save", url.Values{"physics.gravity": {"1"}}, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST without a session: %d", rec.Code)
	}
	if rec := send(h, "GET", "/login", nil, nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `name="password"`) {
		t.Fatalf("login page: %d", rec.Code)
	}
	if rec := send(h, "POST", "/login", url.Values{"email": {"ops@example.com"}, "password": {"wrong-password-123"}}, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", rec.Code)
	}
	rec := send(h, "POST", "/login", url.Values{"email": {"OPS@example.com "}, "password": {"a-long-enough-secret"}}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("sign-in: %d %s", rec.Code, rec.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie: %+v", cookie)
	}
	if rec := send(h, "GET", "/physics", nil, cookie); rec.Code != http.StatusOK {
		t.Fatalf("signed in: %d", rec.Code)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM admin_users WHERE last_login_at IS NOT NULL`); n != 1 {
		t.Fatalf("last_login_at not set")
	}
	if rec := send(h, "POST", "/logout", url.Values{}, cookie); rec.Code != http.StatusSeeOther {
		t.Fatalf("sign-out: %d", rec.Code)
	}
	if rec := send(h, "GET", "/physics", nil, cookie); rec.Code != http.StatusSeeOther {
		t.Fatalf("after sign-out: %d", rec.Code)
	}

	// Production: a sign-in is needed even with no admin
	if _, err := db.Pool.Exec(ctx, `DELETE FROM admin_users`); err != nil {
		t.Fatal(err)
	}
	prod := NewServer(db, s.redis, configsDir()).Production(true).Routes()
	if rec := send(prod, "GET", "/physics", nil, nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("production without an admin: %d", rec.Code)
	}
}

func count(t *testing.T, db *database.PostgresDB, sql string) int {
	t.Helper()
	var n int
	if err := db.Pool.QueryRow(context.Background(), sql).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
