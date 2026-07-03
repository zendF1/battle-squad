package appconfig

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"battle-squad/internal/shared/config"
	"battle-squad/internal/shared/database"

	"github.com/jackc/pgx/v5"
	"gopkg.in/yaml.v3"
)

type Service struct {
	db            *database.PostgresDB
	cfg           *config.Config
	configDir     string
	gameDataCache *GameDataResponse
	gameDataOnce  sync.Once
}

func NewService(db *database.PostgresDB, cfg *config.Config) *Service {
	return &Service{db: db, cfg: cfg, configDir: "configs"}
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

func (s *Service) GetGameData() (*GameDataResponse, error) {
	var loadErr error
	s.gameDataOnce.Do(func() {
		s.gameDataCache, loadErr = s.loadGameData()
	})
	if loadErr != nil {
		// Reset once so next call retries
		s.gameDataOnce = sync.Once{}
		return nil, loadErr
	}
	return s.gameDataCache, nil
}

func (s *Service) loadGameData() (*GameDataResponse, error) {
	resp := &GameDataResponse{
		Characters: make(map[string]CharacterData),
		Weapons:    make(map[string]WeaponData),
		Skills:     make(map[string]SkillData),
		Items:      make(map[string]ItemData),
		Maps:       make(map[string]MapData),
	}

	// Characters
	type charYAML struct {
		CharacterID string `yaml:"characterId"`
		Name        string `yaml:"name"`
		Role        string `yaml:"role"`
		HP          int    `yaml:"hp"`
		Damage      int    `yaml:"damage"`
		Mobility    int    `yaml:"mobility"`
		Defense     int    `yaml:"defense"`
		SkillPower  int    `yaml:"skillPower"`
		Difficulty  int    `yaml:"difficulty"`
		WeaponID    string `yaml:"weaponId"`
		SkillID     string `yaml:"skillId"`
	}
	var chars []charYAML
	if err := s.loadYAML("characters.yaml", &chars); err != nil {
		return nil, err
	}
	for _, c := range chars {
		resp.Characters[c.CharacterID] = CharacterData{
			Name: c.Name, Role: c.Role, HP: c.HP, Damage: c.Damage,
			Mobility: c.Mobility, Defense: c.Defense, SkillPower: c.SkillPower,
			Difficulty: c.Difficulty, WeaponID: c.WeaponID, SkillID: c.SkillID,
		}
	}

	// Weapons
	type weaponYAML struct {
		WeaponID         string  `yaml:"weaponId"`
		Name             string  `yaml:"name"`
		Damage           int     `yaml:"damage"`
		ExplosionRadius  int     `yaml:"explosionRadius"`
		ProjectileWeight float64 `yaml:"projectileWeight"`
		WindInfluence    float64 `yaml:"windInfluence"`
	}
	var weapons []weaponYAML
	if err := s.loadYAML("weapons.yaml", &weapons); err != nil {
		return nil, err
	}
	for _, w := range weapons {
		resp.Weapons[w.WeaponID] = WeaponData{
			Name: w.Name, Damage: w.Damage, ExplosionRadius: w.ExplosionRadius,
			ProjectileWeight: w.ProjectileWeight, WindInfluence: w.WindInfluence,
		}
	}

	// Skills
	type skillYAML struct {
		SkillID          string  `yaml:"skillId"`
		Name             string  `yaml:"name"`
		CooldownTurn     int     `yaml:"cooldownTurn"`
		EffectType       string  `yaml:"effectType"`
		ProjectileCount  int     `yaml:"projectileCount"`
		StatusEffectID   string  `yaml:"statusEffectId"`
		DamageMultiplier float64 `yaml:"damageMultiplier"`
	}
	var skills []skillYAML
	if err := s.loadYAML("skills.yaml", &skills); err != nil {
		return nil, err
	}
	for _, sk := range skills {
		resp.Skills[sk.SkillID] = SkillData{
			Name: sk.Name, CooldownTurn: sk.CooldownTurn, EffectType: sk.EffectType,
			ProjectileCount: sk.ProjectileCount, StatusEffectID: sk.StatusEffectID,
			DamageMultiplier: sk.DamageMultiplier,
		}
	}

	// Items
	type itemYAML struct {
		ItemID         string  `yaml:"itemId"`
		Name           string  `yaml:"name"`
		Type           string  `yaml:"type"`
		TargetType     string  `yaml:"targetType"`
		Value          float64 `yaml:"value"`
		MaxUsePerMatch int     `yaml:"maxUsePerMatch"`
	}
	var items []itemYAML
	if err := s.loadYAML("items.yaml", &items); err != nil {
		return nil, err
	}
	for _, it := range items {
		resp.Items[it.ItemID] = ItemData{
			Name: it.Name, Type: it.Type, TargetType: it.TargetType,
			Value: it.Value, MaxUsePerMatch: it.MaxUsePerMatch,
		}
	}

	// Maps
	type spawnYAML struct {
		X    float64 `yaml:"x"`
		Y    float64 `yaml:"y"`
		Team int     `yaml:"team"`
	}
	type terrainLayerYAML struct {
		Type   string `yaml:"type"`
		YRange []int  `yaml:"yRange"`
	}
	type mapYAML struct {
		MapID                 string             `yaml:"mapId"`
		Name                  string             `yaml:"name"`
		Width                 int                `yaml:"width"`
		Height                int                `yaml:"height"`
		DefaultWindPowerRange []int              `yaml:"defaultWindPowerRange"`
		SpawnPoints           []spawnYAML        `yaml:"spawnPoints"`
		TerrainLayers         []terrainLayerYAML `yaml:"terrainLayers"`
	}
	var maps []mapYAML
	if err := s.loadYAML("maps.yaml", &maps); err != nil {
		return nil, err
	}
	for _, m := range maps {
		spawns := make([]MapSpawnPoint, len(m.SpawnPoints))
		for i, sp := range m.SpawnPoints {
			spawns[i] = MapSpawnPoint{X: sp.X, Y: sp.Y, Team: sp.Team}
		}
		layers := make([]MapTerrainLayer, len(m.TerrainLayers))
		for i, tl := range m.TerrainLayers {
			layers[i] = MapTerrainLayer{Type: tl.Type, YRange: tl.YRange}
		}
		resp.Maps[m.MapID] = MapData{
			Name: m.Name, Width: m.Width, Height: m.Height,
			DefaultWindPowerRange: m.DefaultWindPowerRange,
			SpawnPoints: spawns, TerrainLayers: layers,
		}
	}

	return resp, nil
}

func (s *Service) loadYAML(filename string, target interface{}) error {
	data, err := os.ReadFile(filepath.Join(s.configDir, filename))
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", filename, err)
	}
	if err := yaml.Unmarshal(data, target); err != nil {
		return fmt.Errorf("failed to parse %s: %w", filename, err)
	}
	return nil
}
