package database

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadMigrationsOrdersAndChecksums(t *testing.T) {
	fsys := fstest.MapFS{
		"0002_auth.sql":     {Data: []byte("CREATE TABLE b ();")},
		"0001_identity.sql": {Data: []byte("CREATE TABLE a ();")},
		"migrations.go":     {Data: []byte("package migrations")},
	}

	got, err := LoadMigrations(fsys)
	if err != nil {
		t.Fatalf("LoadMigrations() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (migrations.go must be ignored)", len(got))
	}
	if got[0].Version != 1 || got[0].Name != "identity" {
		t.Errorf("first = %04d_%s, want 0001_identity", got[0].Version, got[0].Name)
	}
	if got[1].Version != 2 {
		t.Errorf("second version = %d, want 2", got[1].Version)
	}
	if len(got[0].Checksum) != 64 {
		t.Errorf("checksum length = %d, want 64 hex chars", len(got[0].Checksum))
	}
	if got[0].Checksum == got[1].Checksum {
		t.Error("different contents must produce different checksums")
	}
}

func TestLoadMigrationsChecksumIsDeterministic(t *testing.T) {
	fsys := fstest.MapFS{"0001_x.sql": {Data: []byte("SELECT 1;")}}
	a, err := LoadMigrations(fsys)
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadMigrations(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if a[0].Checksum != b[0].Checksum {
		t.Error("checksum must be deterministic across loads")
	}
}

func TestLoadMigrationsRejectsBadInput(t *testing.T) {
	cases := []struct {
		name    string
		files   map[string]string
		wantErr string
	}{
		{
			name:    "misnamed file",
			files:   map[string]string{"001_short.sql": "SELECT 1;"},
			wantErr: "does not match",
		},
		{
			name:    "uppercase name",
			files:   map[string]string{"0001_Bad.sql": "SELECT 1;"},
			wantErr: "does not match",
		},
		{
			name:    "gap in versions",
			files:   map[string]string{"0001_a.sql": "SELECT 1;", "0003_c.sql": "SELECT 1;"},
			wantErr: "contiguous",
		},
		{
			name:    "does not start at 0001",
			files:   map[string]string{"0002_a.sql": "SELECT 1;"},
			wantErr: "contiguous",
		},
		{
			name:    "version zero",
			files:   map[string]string{"0000_a.sql": "SELECT 1;"},
			wantErr: "invalid version",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fsys := fstest.MapFS{}
			for name, data := range tc.files {
				fsys[name] = &fstest.MapFile{Data: []byte(data)}
			}
			_, err := LoadMigrations(fsys)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("LoadMigrations() error = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestEmbeddedMigrationsAreValid(t *testing.T) {
	// Guards the real migration directory against naming/numbering mistakes.
	fsys, err := realMigrationsFS()
	if err != nil {
		t.Skipf("embedded migrations unavailable in this test context: %v", err)
	}
	migrations, err := LoadMigrations(fsys)
	if err != nil {
		t.Fatalf("embedded migrations are invalid: %v", err)
	}
	if len(migrations) < 3 {
		t.Errorf("expected at least 3 embedded migrations, got %d", len(migrations))
	}
}
