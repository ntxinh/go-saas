// Package migrations embeds the goose SQL migrations so
// database.Migrate and the goose CLI share one source of truth.
// It lives at repo root because go:embed cannot reach ../migrations.
package migrations

import "embed"

// FS is the embedded migrations directory (goose SQL files).
//
//go:embed *.sql
var FS embed.FS
