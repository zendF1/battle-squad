package appconfig

import "time"

type ClientVersionPolicy struct {
	Platform            string    `json:"platform"`
	MinSupportedVersion string    `json:"minSupportedVersion"`
	LatestVersion       string    `json:"latestVersion"`
	ProtocolVersion     int       `json:"protocolVersion"`
	ForceUpdate         bool      `json:"forceUpdate"`
	SoftUpdateMessage   *string   `json:"softUpdateMessage,omitempty"`
	StoreURL            string    `json:"storeUrl"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type GameDataResponse struct {
	Characters map[string]CharacterData `json:"characters"`
	Weapons    map[string]WeaponData    `json:"weapons"`
	Skills     map[string]SkillData     `json:"skills"`
	Items      map[string]ItemData      `json:"items"`
	Maps       map[string]MapData       `json:"maps"`
}

type CharacterData struct {
	Name       string `json:"name"`
	Role       string `json:"role"`
	HP         int    `json:"hp"`
	Damage     int    `json:"damage"`
	Mobility   int    `json:"mobility"`
	Defense    int    `json:"defense"`
	SkillPower int    `json:"skillPower"`
	Difficulty int    `json:"difficulty"`
	WeaponID   string `json:"weaponId"`
	SkillID    string `json:"skillId"`
}

type WeaponData struct {
	Name            string  `json:"name"`
	Damage          int     `json:"damage"`
	ExplosionRadius int     `json:"explosionRadius"`
	ProjectileWeight float64 `json:"projectileWeight"`
	WindInfluence   float64 `json:"windInfluence"`
}

type SkillData struct {
	Name             string  `json:"name"`
	CooldownTurn     int     `json:"cooldownTurn"`
	EffectType       string  `json:"effectType"`
	ProjectileCount  int     `json:"projectileCount,omitempty"`
	StatusEffectID   string  `json:"statusEffectId,omitempty"`
	DamageMultiplier float64 `json:"damageMultiplier"`
}

type ItemData struct {
	Name           string  `json:"name"`
	Type           string  `json:"type"`
	TargetType     string  `json:"targetType"`
	Value          float64 `json:"value"`
	MaxUsePerMatch int     `json:"maxUsePerMatch"`
}

type MapSpawnPoint struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Team int     `json:"team,omitempty"`
}

type MapTerrainLayer struct {
	Type   string `json:"type"`
	YRange []int  `json:"yRange"`
}

type MapData struct {
	Name                  string            `json:"name"`
	Width                 int               `json:"width"`
	Height                int               `json:"height"`
	DefaultWindPowerRange []int             `json:"defaultWindPowerRange"`
	SpawnPoints           []MapSpawnPoint   `json:"spawnPoints"`
	TerrainLayers         []MapTerrainLayer `json:"terrainLayers"`
}

type RemoteConfig struct {
	APIUrl                 string            `json:"apiUrl"`
	GameWSUrl              string            `json:"gameWsUrl"`
	DefaultCharacterSelect string            `json:"defaultCharacterSelect"`
	ShopEnabled            bool              `json:"shopEnabled"`
	ActiveItems            []string          `json:"activeItems"`
	MaintenanceMode        bool              `json:"maintenanceMode"`
	ClientParams           map[string]string `json:"clientParams"`
}
