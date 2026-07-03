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
	APIUrl                 string            `json:"apiUrl"`
	GameWSUrl              string            `json:"gameWsUrl"`
	DefaultCharacterSelect string            `json:"defaultCharacterSelect"`
	ShopEnabled            bool              `json:"shopEnabled"`
	ActiveItems            []string          `json:"activeItems"`
	MaintenanceMode        bool              `json:"maintenanceMode"`
	ClientParams           map[string]string `json:"clientParams"`
}
