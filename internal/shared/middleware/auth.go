package middleware

import (
	"context"
	"net/http"
	"strings"

	"battle-squad/internal/shared/auth"
	"battle-squad/internal/shared/model"
	"battle-squad/internal/shared/observability"
)

func AuthMiddleware(jwtManager *auth.JWTManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				model.WriteError(w, r, model.ErrUnauthorized)
				return
			}

			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				model.WriteError(w, r, model.ErrUnauthorized)
				return
			}

			tokenStr := parts[1]
			claims, err := jwtManager.Verify(tokenStr)
			if err != nil {
				model.WriteError(w, r, model.ErrUnauthorized)
				return
			}

			// Add account, player ID, and role to context
			ctx := context.WithValue(r.Context(), observability.PlayerIDKey, claims.PlayerID)
			ctx = context.WithValue(ctx, "accountId", claims.AccountID)
			ctx = context.WithValue(ctx, observability.RoleKey, claims.Role)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
