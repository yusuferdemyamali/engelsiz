package database

import (
	"bytes"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
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
	if count != 4 {
		t.Fatalf("expected 4 applied migrations, got %d", count)
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
	name := "special ?# metadata.db"
	if runtime.GOOS == "windows" {
		// Windows disallows question marks in filenames; percent/hash still exercise URI escaping.
		name = "special %# Türkçe metadata.db"
	}
	path := filepath.Join(t.TempDir(), name)
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
	for _, name := range []string{"001_init.sql", "002_downloads.sql", "003_packages.sql"} {
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

func TestUpgradeLegacyDownloadPreservesHistoryWithoutInventingTrust(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", sqliteURI(path))
	if err != nil {
		t.Fatal(err)
	}
	oldMigrations := fstest.MapFS{}
	for _, name := range []string{"001_init.sql", "002_downloads.sql"} {
		body, err := embeddedMigrations.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		oldMigrations["migrations/"+name] = &fstest.MapFile{Data: body}
	}
	if err := Migrate(legacy, oldMigrations, "migrations"); err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`INSERT INTO downloads
		(id,provider,provider_package_id,title,variant,file_name,source_url,temp_path,total_bytes,downloaded_bytes,status,created_at,updated_at)
		VALUES ('legacy','kiwix','old-release','Eski Vikipedi','mini','old.zim','https://old.example/old.zim','/private/legacy.part',7,7,'completed','2026-01-01T00:00:00Z','2026-01-02T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = legacy.Exec(`INSERT INTO settings(key,value) VALUES('preserve_upgrade','yes')`); err != nil {
		t.Fatal(err)
	}
	if err = legacy.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	var status, title, source, temp, artifactState string
	var total, transferred int64
	var contentType, language, version, algorithm, checksum, owner sql.NullString
	var expected sql.NullInt64
	err = upgraded.QueryRow(`SELECT status,title,source_url,temp_path,total_bytes,downloaded_bytes,
		content_type,language,version,expected_size,checksum_algorithm,checksum_value,artifact_owner,artifact_state
		FROM downloads WHERE id='legacy'`).Scan(&status, &title, &source, &temp, &total, &transferred,
		&contentType, &language, &version, &expected, &algorithm, &checksum, &owner, &artifactState)
	if err != nil {
		t.Fatal(err)
	}
	if status != "completed" || title != "Eski Vikipedi" || source != "https://old.example/old.zim" || temp != "/private/legacy.part" || total != 7 || transferred != 7 || artifactState != "available" {
		t.Fatalf("legacy history changed: status=%s title=%s source=%s temp=%s total=%d transferred=%d ownership=%s", status, title, source, temp, total, transferred, artifactState)
	}
	for name, value := range map[string]sql.NullString{"content_type": contentType, "language": language, "version": version, "checksum_algorithm": algorithm, "checksum_value": checksum, "artifact_owner": owner} {
		if value.Valid {
			t.Fatalf("migration invented %s=%q", name, value.String)
		}
	}
	if expected.Valid {
		t.Fatalf("migration treated historical transfer total as trusted expected size: %d", expected.Int64)
	}
	var count int
	if err := upgraded.QueryRow(`SELECT COUNT(*) FROM content_packages`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("migration invented installed packages: count=%d err=%v", count, err)
	}
	var setting string
	if err := upgraded.QueryRow(`SELECT value FROM settings WHERE key='preserve_upgrade'`).Scan(&setting); err != nil || setting != "yes" {
		t.Fatalf("unrelated metadata lost: %q %v", setting, err)
	}
}
