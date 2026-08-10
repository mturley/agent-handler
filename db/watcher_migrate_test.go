package db

import (
	"path/filepath"
	"testing"

	wdb "github.com/mturley/watcher/db"
)

func TestOpenRunsWatcherMigrate(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()
	v, err := wdb.SchemaVersion(d.Conn())
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if v < 1 {
		t.Fatalf("watcher schema not migrated: version %d", v)
	}
}
