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

type RemoteConfig struct {
	APIUrl                 string   `json:"apiUrl"`
	GameWSUrl              string   `json:"gameWsUrl"`
	DefaultCharacterSelect string   `json:"defaultCharacterSelect"`
	ShopEnabled            bool     `json:"shopEnabled"`
	ActiveItems            []string `json:"activeItems"`
	MaintenanceMode        bool     `json:"maintenanceMode"`
	// ClientParams are kept for old clients: windScaleFactor and physicsTimeStep of the game
	// data (physics.wind_scale, physics.time_step). The rules are GET /gamedata.
	ClientParams map[string]string `json:"clientParams"`
	// GamedataVersion is the GET /gamedata version of the game data the API serves.
	GamedataVersion string `json:"gamedataVersion"`
}
