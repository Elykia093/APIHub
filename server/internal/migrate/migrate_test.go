package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type migrationFixture struct {
	Migrations []struct {
		Version int    `json:"version"`
		SHA256  string `json:"sha256"`
		Bytes   int    `json:"bytes"`
	} `json:"migrations"`
}

func TestMigrationBytesMatchNodeGoldens(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	payload, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "tests", "fixtures", "compatibility-vectors.json"))
	if err != nil {
		t.Fatalf("read compatibility fixture: %v", err)
	}
	var fixture migrationFixture
	if err := json.Unmarshal(payload, &fixture); err != nil {
		t.Fatalf("decode compatibility fixture: %v", err)
	}
	actual := All()
	if len(actual) != len(fixture.Migrations) {
		t.Fatalf("migration count = %d, want %d", len(actual), len(fixture.Migrations))
	}
	for index, migration := range actual {
		want := fixture.Migrations[index]
		if migration.Version != want.Version || Checksum(migration.SQL) != want.SHA256 || len([]byte(migration.SQL)) != want.Bytes {
			t.Fatalf("migration %d mismatch: version=%d checksum=%s bytes=%d", index, migration.Version, Checksum(migration.SQL), len([]byte(migration.SQL)))
		}
	}
}
