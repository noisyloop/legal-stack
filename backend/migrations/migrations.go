// Package migrations embeds the SQL schema migrations into the binary so a
// deployed artifact and its schema can never drift apart (ADR-0011).
package migrations

import "embed"

// FS contains every numbered migration file, applied in order at boot.
//
//go:embed *.sql
var FS embed.FS
