package database

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestMigrateFreshAndIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"settings", "providers", "schema_migrations"} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil || n != 1 {
			t.Fatalf("table %s missing: %v", table, err)
		}
	}
	db.Close()

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen (idempotent): %v", err)
	}
	var count int
	if err := db2.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2 applied migrations, got %d", count)
	}
	db2.Close()
}

func TestCorruptDBNoReset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.db")
	if err := os.WriteFile(path, []byte("this is not a sqlite file at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Open(path)
	if err == nil {
		t.Fatal("expected error for corrupt db, got nil")
	}
	body, readErr := os.ReadFile(path)
	if readErr != nil || !bytes.Equal(body, []byte("this is not a sqlite file at all")) {
		t.Fatalf("corrupt file changed: %q %v", body, readErr)
	}
	st, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatalf("corrupt db file was deleted: %v", statErr)
	}
	if st.Size() == 0 {
		t.Fatal("corrupt db file was truncated")
	}
}

func TestSQLitePragmasAndMetadataPreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "special ?# metadata.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var mode string
	var timeout, keys int
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("mode %q %v", mode, err)
	}
	if err := db.QueryRow("PRAGMA busy_timeout").Scan(&timeout); err != nil || timeout != 5000 {
		t.Fatalf("timeout %d %v", timeout, err)
	}
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&keys); err != nil || keys != 1 {
		t.Fatalf("foreign keys %d %v", keys, err)
	}
	if _, err := db.Exec("INSERT INTO settings(key,value) VALUES('test','preserved')"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var value string
	if err := db.QueryRow("SELECT value FROM settings WHERE key='test'").Scan(&value); err != nil || value != "preserved" {
		t.Fatalf("metadata %q %v", value, err)
	}
}

func TestFailedMigrationRollsBack(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fixture := fstest.MapFS{"migrations/002_broken.sql": &fstest.MapFile{Data: []byte("CREATE TABLE partial(id INTEGER); INSERT INTO nonexistent VALUES(1);")}}
	if err := Migrate(db, fixture, "migrations"); err == nil {
		t.Fatal("broken migration succeeded")
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='partial'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial migration persisted: %d %v", count, err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version='002_broken.sql'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed migration recorded: %d %v", count, err)
	}
}

func TestRootMigrationMatchesEmbedded(t *testing.T) {
	for _, name := range []string{"001_init.sql", "002_downloads.sql"} {
		root, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		embedded, err := embeddedMigrations.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(root, embedded) {
			t.Fatalf("root and embedded migrations differ: %s", name)
		}
	}
}

func TestWindowsDriveSQLiteURI(t *testing.T) {
	uri, err := url.Parse(sqliteURI("C:/Users/Test User/Engelsiz/special ?# metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	if uri.Scheme != "file" || uri.Host != "" || uri.Path != "/C:/Users/Test User/Engelsiz/special ?# metadata.db" {
		t.Fatalf("Windows SQLite URI has invalid authority/path: %+v", uri)
	}
	if got := uri.Query()["_pragma"]; len(got) != 2 {
		t.Fatalf("pragmas lost: %v", got)
	}
}
