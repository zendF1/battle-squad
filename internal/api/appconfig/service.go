package appconfig

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"battle-squad/internal/shared/config"
	"battle-squad/internal/shared/database"

	"github.com/jackc/pgx/v5"
)

type Service struct {
	db  *database.PostgresDB
	cfg *config.Config
}

func NewService(db *database.PostgresDB, cfg *config.Config) *Service {
	return &Service{db: db, cfg: cfg}
}

func (s *Service) GetVersionPolicy(ctx context.Context, platform string) (*ClientVersionPolicy, error) {
	if platform != "android" && platform != "ios" {
		platform = "android" // default fallback
	}

	query := `
		SELECT platform, min_supported_version, latest_version, protocol_version, force_update, soft_update_message, store_url, updated_at
		FROM client_version_policies
		WHERE platform = $1
	`
	var p ClientVersionPolicy
	var msg sql.NullString
	err := s.db.Pool.QueryRow(ctx, query, platform).Scan(
		&p.Platform,
		&p.MinSupportedVersion,
		&p.LatestVersion,
		&p.ProtocolVersion,
		&p.ForceUpdate,
		&msg,
		&p.StoreURL,
		&p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Mock default fallback
			storeURL := "https://play.google.com/store/apps/details?id=com.battlesquad"
			if platform == "ios" {
				storeURL = "https://apps.apple.com/app/battle-squad/id12345"
			}
			return &ClientVersionPolicy{
				Platform:            platform,
				MinSupportedVersion: "1.0.0",
				LatestVersion:       s.cfg.AppVersion,
				ProtocolVersion:     s.cfg.ProtocolVersion,
				ForceUpdate:         false,
				StoreURL:            storeURL,
				UpdatedAt:           time.Now(),
			}, nil
		}
		return nil, err
	}

	if msg.Valid {
		p.SoftUpdateMessage = &msg.String
	}
	return &p, nil
}

func (s *Service) GetRemoteConfig(ctx context.Context) (*RemoteConfig, error) {
	// Expose current backend API and WS connection endpoints
	return &RemoteConfig{
		APIUrl:                 "http://localhost:" + s.cfg.APIPort,
		GameWSUrl:              "ws://localhost:" + s.cfg.GamePort + "/ws",
		DefaultCharacterSelect: "rookie",
		ShopEnabled:            true,
		ActiveItems:            []string{"medkit", "teleport", "power_shot", "drill_bomb", "spider_net", "freeze_bomb", "air_strike", "wind_stopper"},
		MaintenanceMode:        false,
		ClientParams: map[string]string{
			"windScaleFactor": "30.0",
			"physicsTimeStep": "0.05",
		},
	}, nil
}
