// Package configs embeds the reference game data YAML (the seed's source), so the migrate
// binary needs no checkout next to it.
package configs

import "embed"

// FS holds every *.yaml file.
//
//go:embed *.yaml
var FS embed.FS
