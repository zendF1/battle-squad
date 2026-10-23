package admin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"html/template"
	"net/http"
	"strings"
	"time"

	"battle-squad/internal/shared/auth"
	"battle-squad/internal/shared/observability"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/redis/go-redis/v9"
)

// Dashboard logins: admin_users rows (cmd/migrate bootstraps the first from
// ADMIN_BOOTSTRAP_EMAIL / ADMIN_BOOTSTRAP_PASSWORD), sessions in Redis behind an HttpOnly
// cookie. Production always requires a login; elsewhere the dashboard stays open until the
// first admin exists (the dev flow before any admin was created).
const (
	sessionCookie   = "bs_admin_session"
	sessionTTL      = 12 * time.Hour
	sessionPrefix   = "admin:session:"
	loginFailPrefix = "admin:login_fail:"
	loginFailLimit  = 10
	loginFailWindow = 15 * time.Minute
)

// dummyHash is compared when the email is unknown.
var dummyHash, _ = auth.HashAdminPassword("unknown-admin-email-placeholder")

type adminCtxKey struct{}

// adminEmail is the signed-in admin of the request ("" when the dashboard is open).
func adminEmail(r *http.Request) string {
	v, _ := r.Context().Value(adminCtxKey{}).(string)
	return v
}

// Production makes every page require a login, even before the first admin exists.
func (s *Server) Production(production bool) *Server {
	s.production = production
	return s
}

// WarnIfNoAdmin logs at startup when nobody can sign in, or the dashboard is open.
func (s *Server) WarnIfNoAdmin(ctx context.Context) {
	var count int
	if err := s.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_users`).Scan(&count); err != nil {
		observability.Log.Warn().Err(err).Msg("cannot read admin_users (run cmd/migrate)")
		return
	}
	if count > 0 {
		return
	}
	if s.production {
		observability.Log.Warn().Msg("no admin login exists: nobody can sign in until cmd/migrate runs with ADMIN_BOOTSTRAP_EMAIL / ADMIN_BOOTSTRAP_PASSWORD")
		return
	}
	observability.Log.Warn().Msg("no admin login exists: the dashboard is open without sign-in (development only)")
}

// loginRequired: production, or at least one admin exists.
func (s *Server) loginRequired(ctx context.Context) (bool, error) {
	if s.production {
		return true, nil
	}
	var exists bool
	err := s.db.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM admin_users)`).Scan(&exists)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "42P01" { // no admin_users yet (migration 056 not run)
		return false, nil
	}
	return exists, err
}

func sessionKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return sessionPrefix + hex.EncodeToString(sum[:])
}

// requireLogin sends a request without a valid session to /login (static files and the login
// page itself excepted).
func (s *Server) requireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" || strings.HasPrefix(r.URL.Path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}
		required, err := s.loginRequired(r.Context())
		if err != nil {
			observability.Log.Error().Err(err).Msg("admin login check failed")
			http.Error(w, "admin login check failed", http.StatusServiceUnavailable)
			return
		}
		if !required {
			next.ServeHTTP(w, r)
			return
		}
		if email := s.sessionEmail(r); email != "" {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), adminCtxKey{}, email)))
			return
		}
		if r.Method == http.MethodGet && !strings.HasPrefix(r.URL.Path, "/api/") {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		http.Error(w, "admin login required", http.StatusUnauthorized)
	})
}

func (s *Server) sessionEmail(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" || s.redis == nil {
		return ""
	}
	email, err := s.redis.Client.Get(r.Context(), sessionKey(c.Value)).Result()
	if err != nil {
		return ""
	}
	return email
}

// secureRequest: the cookie is Secure when the dashboard is served over HTTPS (directly or
// behind a proxy setting X-Forwarded-Proto).
func secureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

var loginTmpl = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html lang="en"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Battle Squad Admin</title>
<style>
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #f5f5f5; color: #333; display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; }
form { background: #fff; padding: 28px; border-radius: 8px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); width: 320px; }
h1 { font-size: 18px; color: #1a1a2e; margin: 0 0 18px; text-align: center; }
label { display: block; font-weight: 600; font-size: 13px; margin: 12px 0 4px; }
input { width: 100%; box-sizing: border-box; padding: 8px 10px; border: 1px solid #ccc; border-radius: 4px; font-size: 14px; }
button { margin-top: 18px; width: 100%; padding: 9px; background: #4a6cf7; color: #fff; border: none; border-radius: 4px; font-size: 14px; cursor: pointer; }
.err { background: #f8d7da; color: #721c24; padding: 8px 10px; border-radius: 4px; font-size: 13px; }
</style></head><body>
<form method="POST" action="/login">
<h1>Battle Squad Admin</h1>
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}
<label for="email">Email</label><input id="email" name="email" type="email" autocomplete="username" required value="{{.Email}}">
<label for="password">Password</label><input id="password" name="password" type="password" autocomplete="current-password" required>
<button type="submit">Sign in</button>
</form></body></html>`))

func (s *Server) renderLogin(w http.ResponseWriter, status int, email, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := loginTmpl.Execute(w, map[string]string{"Email": email, "Error": errMsg}); err != nil {
		observability.Log.Error().Err(err).Msg("login page render failed")
	}
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	s.renderLogin(w, http.StatusOK, "", "")
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	email := auth.NormalizeAdminEmail(r.FormValue("email"))
	password := r.FormValue("password")
	if s.redis == nil {
		s.renderLogin(w, http.StatusServiceUnavailable, email, "Sessions need Redis")
		return
	}
	failKey := loginFailPrefix + email
	if n, _ := s.redis.Client.Get(ctx, failKey).Int(); n >= loginFailLimit {
		s.renderLogin(w, http.StatusTooManyRequests, email, "Too many failed sign-ins, try again later")
		return
	}
	var adminID int64
	var hash string
	err := s.db.Pool.QueryRow(ctx, `SELECT admin_id, password_hash FROM admin_users WHERE email = $1`, email).Scan(&adminID, &hash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		observability.Log.Error().Err(err).Msg("admin login lookup failed")
		s.renderLogin(w, http.StatusServiceUnavailable, email, "Sign-in failed, try again")
		return
	}
	if err != nil {
		hash = dummyHash // the same bcrypt cost whether the email exists or not
	}
	if !auth.CheckAdminPassword(hash, password) || err != nil {
		pipe := s.redis.Client.TxPipeline()
		pipe.Incr(ctx, failKey)
		pipe.Expire(ctx, failKey, loginFailWindow)
		pipe.Exec(ctx)
		observability.Log.Warn().Str("email", email).Str("ip", r.RemoteAddr).Msg("admin sign-in failed")
		s.renderLogin(w, http.StatusUnauthorized, email, "Wrong email or password")
		return
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		s.renderLogin(w, http.StatusInternalServerError, email, "Sign-in failed, try again")
		return
	}
	token := hex.EncodeToString(raw)
	if err := s.redis.Client.Set(ctx, sessionKey(token), email, sessionTTL).Err(); err != nil {
		observability.Log.Error().Err(err).Msg("admin session store failed")
		s.renderLogin(w, http.StatusServiceUnavailable, email, "Sign-in failed, try again")
		return
	}
	s.redis.Client.Del(ctx, failKey)
	if _, err := s.db.Pool.Exec(ctx, `UPDATE admin_users SET last_login_at = CURRENT_TIMESTAMP WHERE admin_id = $1`, adminID); err != nil {
		observability.Log.Warn().Err(err).Msg("admin last_login_at update failed")
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", MaxAge: int(sessionTTL.Seconds()),
		HttpOnly: true, Secure: secureRequest(r), SameSite: http.SameSiteLaxMode})
	observability.Log.Info().Str("email", email).Msg("admin signed in")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" && s.redis != nil {
		if err := s.redis.Client.Del(r.Context(), sessionKey(c.Value)).Err(); err != nil && !errors.Is(err, redis.Nil) {
			observability.Log.Warn().Err(err).Msg("admin session delete failed")
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: secureRequest(r), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
