package observability

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

type contextKey string

const (
	CorrelationIDKey contextKey = "correlation_id"
	PlayerIDKey      contextKey = "player_id"
	MatchIDKey       contextKey = "match_id"
	RoleKey          contextKey = "role"
)

var Log zerolog.Logger

func InitLogger(env string) {
	var output io.Writer = os.Stdout

	if env == "development" {
		output = zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
		}
	}

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	
	Log = zerolog.New(output).With().
		Timestamp().
		Str("env", env).
		Logger()
}

func FromContext(ctx context.Context) zerolog.Logger {
	logger := Log
	
	if corrID, ok := ctx.Value(CorrelationIDKey).(string); ok && corrID != "" {
		logger = logger.With().Str("correlation_id", corrID).Logger()
	}
	if playerID, ok := ctx.Value(PlayerIDKey).(string); ok && playerID != "" {
		logger = logger.With().Str("player_id", playerID).Logger()
	}
	if matchID, ok := ctx.Value(MatchIDKey).(string); ok && matchID != "" {
		logger = logger.With().Str("match_id", matchID).Logger()
	}
	
	return logger
}
