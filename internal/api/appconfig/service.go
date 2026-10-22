package appconfig

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strconv"
	"time"

	"battle-squad/internal/game/gamedata"
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

// GetRemoteConfig is the remote config. Its game values come from the game data snapshot the
// API serves (the same one GET /gamedata is built from, whose version it carries); the full
// rules are GET /gamedata.
func (s *Service) GetRemoteConfig(ctx context.Context) (*RemoteConfig, error) {
	gd := gamedata.Current()
	items := make([]gamedata.ItemConfig, 0, len(gd.Data.Items))
	for _, it := range gd.Data.Items {
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].SortOrder != items[j].SortOrder {
			return items[i].SortOrder < items[j].SortOrder
		}
		return items[i].ItemID < items[j].ItemID
	})
	active := make([]string, 0, len(items))
	for _, it := range items {
		active = append(active, it.ItemID)
	}
	// Expose current backend API and WS connection endpoints
	return &RemoteConfig{
		APIUrl:                 "http://localhost:" + s.cfg.APIPort,
		GameWSUrl:              "ws://localhost:" + s.cfg.GamePort + "/ws",
		DefaultCharacterSelect: gd.Rules.Player.DefaultCharacterID,
		ShopEnabled:            true,
		ActiveItems:            active,
		MaintenanceMode:        false,
		ClientParams: map[string]string{
			"windScaleFactor": strconv.FormatFloat(gd.Physics.WindScale, 'f', -1, 64),
			"physicsTimeStep": strconv.FormatFloat(gd.Physics.TimeStep, 'f', -1, 64),
		},
		GamedataVersion: gd.Version,
	}, nil
}
