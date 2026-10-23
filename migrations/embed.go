// Package migrations embeds the up migrations, so the migrate binary needs no checkout next to it.
package migrations

import "embed"

// FS holds every *.up.sql file; internal/shared/migrate applies them in filename order.
//
//go:embed *.up.sql
var FS embed.FS
