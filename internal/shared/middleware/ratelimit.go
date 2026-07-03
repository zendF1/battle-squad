package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"

	"battle-squad/internal/shared/model"
	"golang.org/x/time/rate"
)

type client struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type RateLimiter struct {
	sync.Mutex
	clients map[string]*client
	rate    rate.Limit
	burst   int
}

func NewRateLimiter(r rate.Limit, b int) *RateLimiter {
	rl := &RateLimiter{
		clients: make(map[string]*client),
		rate:    r,
		burst:   b,
	}

	// Clean up stale clients periodically
	go rl.cleanup()

	return rl
}

func (rl *RateLimiter) Limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}

		limiter := rl.getLimiter(ip)
		if !limiter.Allow() {
			errLimit := model.AppError{
				Code:    "RATE_LIMIT_EXCEEDED",
				Message: "Too many requests. Please slow down.",
				Status:  http.StatusTooManyRequests,
			}
			model.WriteError(w, r, errLimit)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (rl *RateLimiter) getLimiter(ip string) *rate.Limiter {
	rl.Lock()
	defer rl.Unlock()

	c, exists := rl.clients[ip]
	if !exists {
		limiter := rate.NewLimiter(rl.rate, rl.burst)
		rl.clients[ip] = &client{
			limiter:  limiter,
			lastSeen: time.Now(),
		}
		return limiter
	}

	c.lastSeen = time.Now()
	return c.limiter
}

func (rl *RateLimiter) cleanup() {
	for {
		time.Sleep(1 * time.Minute)

		rl.Lock()
		for ip, c := range rl.clients {
			if time.Since(c.lastSeen) > 5*time.Minute {
				delete(rl.clients, ip)
			}
		}
		rl.Unlock()
	}
}
