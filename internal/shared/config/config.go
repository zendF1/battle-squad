package config

import (
	"os"
	"strconv"
)

type Config struct {
	Env            string
	APIPort        string
	GamePort       string
	PostgresDSN    string
	RedisAddr      string
	RedisPassword  string
	RedisDB        int
	JWTSecret      string
	AppVersion     string
	ProtocolVersion int
}

func LoadConfig() *Config {
	env := getEnv("APP_ENV", "development")
	apiPort := getEnv("API_PORT", "8080")
	gamePort := getEnv("GAME_PORT", "8081")
	postgresDSN := getEnv("POSTGRES_DSN", "postgres://postgres:postgres@localhost:5432/battlesquad?sslmode=disable")
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	redisPassword := getEnv("REDIS_PASSWORD", "")
	
	redisDBStr := getEnv("REDIS_DB", "0")
	redisDB, err := strconv.Atoi(redisDBStr)
	if err != nil {
		redisDB = 0
	}
	
	jwtSecret := getEnv("JWT_SECRET", "super-secret-battle-squad-key-2026")
	appVersion := getEnv("APP_VERSION", "1.0.0")
	
	protocolVersionStr := getEnv("PROTOCOL_VERSION", "1")
	protocolVersion, err := strconv.Atoi(protocolVersionStr)
	if err != nil {
		protocolVersion = 1
	}

	return &Config{
		Env:             env,
		APIPort:         apiPort,
		GamePort:        gamePort,
		PostgresDSN:     postgresDSN,
		RedisAddr:       redisAddr,
		RedisPassword:   redisPassword,
		RedisDB:         redisDB,
		JWTSecret:       jwtSecret,
		AppVersion:      appVersion,
		ProtocolVersion: protocolVersion,
	}
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}
