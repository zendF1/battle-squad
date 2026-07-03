package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"battle-squad/internal/shared/observability"
)

func CorrelationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corrID := r.Header.Get("X-Correlation-ID")
		if corrID == "" {
			corrID = generateID()
		}

		ctx := context.WithValue(r.Context(), observability.CorrelationIDKey, corrID)
		w.Header().Set("X-Correlation-ID", corrID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func generateID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "fallback-uuid"
	}
	return hex.EncodeToString(bytes)
}
