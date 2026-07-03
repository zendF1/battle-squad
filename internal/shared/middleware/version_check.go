package middleware

import (
	"net/http"
	"strconv"

	"battle-squad/internal/shared/config"
	"battle-squad/internal/shared/model"
)

func VersionCheck(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Bypass version checking for health checks
			path := r.URL.Path
			if path == "/healthz" || path == "/readyz" || path == "/livez" {
				next.ServeHTTP(w, r)
				return
			}

			clientProtocolStr := r.Header.Get("X-Protocol-Version")
			if clientProtocolStr == "" {
				next.ServeHTTP(w, r)
				return
			}

			clientProtocol, err := strconv.Atoi(clientProtocolStr)
			if err != nil {
				model.WriteError(w, r, model.ErrBadRequest)
				return
			}

			// If client protocol version is strictly smaller than minimum supported protocol version,
			// trigger force update
			if clientProtocol < cfg.ProtocolVersion {
				model.WriteError(w, r, model.ErrForceUpdate)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
