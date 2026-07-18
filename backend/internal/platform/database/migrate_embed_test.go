package database

import (
	"io/fs"

	"github.com/noisyloop/legal-stack/backend/migrations"
)

// realMigrationsFS exposes the embedded production migrations to tests so
// naming/numbering mistakes fail CI before they fail a deploy.
func realMigrationsFS() (fs.FS, error) {
	return migrations.FS, nil
}
