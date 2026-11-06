package packages

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"engelsiz/internal/database"
	"engelsiz/internal/download"
)

func eduSeed(t *testing.T, dir string, db *sql.DB, m *download.Manager, id, pkg, contentType string, body []byte) {
	t.Helper()
	sum := sha256.Sum256(body)
	stamp := now()
	path := filepath.Join(dir, "downloads", id+".part")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := db.Exec(`INSERT INTO downloads(id,provider,provider_package_id,title,variant,file_name,source_url,temp_path,total_bytes,downloaded_bytes,status,created_at,updated_at,content_type,language,version,expected_size,checksum_algorithm,checksum_value) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, "kiwix", pkg, "Edu "+pkg, "edu-"+pkg, "edu.zim", "https://upstream.invalid/private", path, len(body), len(body), "completed", stamp, stamp, contentType, "tr", "2026-10-01", len(body), "sha256", hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
}

func eduServices(t *testing.T) (string, *sql.DB, *Service, *download.Manager, *int) {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	notified := 0
	s, err := New(context.Background(), db, dir, Options{OnInstalled: func() { notified++ }})
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	m, err := download.New(db, filepath.Join(dir, "downloads"), download.Options{})
	if err != nil {
		s.Close()
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.Close()
		m.Close()
		db.Close()
	})
	return dir, db, s, m, &notified
}

func waitPkg(t *testing.T, s *Service, id, status string) record {
	t.Helper()
	limit := time.Now().Add(5 * time.Second)
	for time.Now().Before(limit) {
		r, err := s.get(id)
		if err == nil && r.Status == status {
			return r
		}
		time.Sleep(time.Millisecond)
	}
	r, err := s.get(id)
	t.Fatalf("wanted %s got %+v %v", status, r, err)
	return record{}
}

func isPkgCode(err error, code string) bool {
	var problem *download.Error
	return errors.As(err, &problem) && problem.Code == code
}

// RemoveEducation must refuse every non-education record without touching
// anything: Wikipedia rows, files and activation survive.
func TestRemoveEducationRefusesNoneducation(t *testing.T) {
	_, _, _, _, _ = eduServices(t)
	// Seed a Wikipedia install through the existing fixture path.
	f := newFixture(t, Options{})
	p, _, err := f.s.Install(f.m, testID)
	if err != nil {
		t.Fatal(err)
	}
	_ = p
	installed := waitPkg(t, f.s, testID, "installed")
	if _, err := os.Stat(installed.LocalPath); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM active_packages WHERE package_id=?`, testID).Scan(&before); err != nil || before != 1 {
		t.Fatalf("activation %d %v", before, err)
	}
	if err := f.s.RemoveEducation(testID); !isPkgCode(err, "invalid_request") {
		t.Fatalf("wikipedia removal must be refused, got %v", err)
	}
	after, err := f.s.get(testID)
	if err != nil || after.Status != "installed" {
		t.Fatalf("wikipedia row disturbed %+v %v", after, err)
	}
	if _, err := os.Stat(installed.LocalPath); err != nil {
		t.Fatalf("wikipedia file disturbed: %v", err)
	}
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM active_packages WHERE package_id=?`, testID).Scan(&before); err != nil || before != 1 {
		t.Fatalf("activation disturbed %d %v", before, err)
	}
}

// RemoveEducation removes an installed educational ZIM: activation row, then
// file, then record, then notification. Unknown ids are not_found.
func TestRemoveEducationRoundTrip(t *testing.T) {
	dir, db, s, m, notified := eduServices(t)
	id := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	body := []byte("education zim bytes")
	eduSeed(t, dir, db, m, id, "afet-ilk-yardim", "education", body)
	if _, _, err := s.Install(m, id); err != nil {
		t.Fatal(err)
	}
	installed := waitPkg(t, s, id, "installed")
	var active int
	if err := db.QueryRow(`SELECT COUNT(*) FROM active_packages WHERE package_id=?`, id).Scan(&active); err != nil || active != 1 {
		t.Fatalf("activation %d %v", active, err)
	}
	if err := s.RemoveEducation(id); err != nil {
		t.Fatal(err)
	}
	if *notified == 0 {
		t.Fatal("expected OnInstalled notification after removal")
	}
	if files, err := s.InstalledSnapshot(); err != nil || len(files) != 0 {
		t.Fatalf("snapshot after removal %+v %v", files, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "content", "kiwix", id+".zim")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("zim file remains: %v", err)
	}
	if _, err := s.get(id); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("record remains: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM active_packages WHERE package_id=?`, id).Scan(&active); err != nil || active != 0 {
		t.Fatalf("activation remains %d %v", active, err)
	}
	// Unrelated files are preserved.
	if _, err := os.Stat(installed.LocalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed path wrong: %v", err)
	}
	if err := s.RemoveEducation(id); !isPkgCode(err, "not_found") {
		t.Fatalf("second removal: %v", err)
	}
	if err := s.RemoveEducation("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); !isPkgCode(err, "not_found") {
		t.Fatalf("unknown removal: %v", err)
	}
}

// RemoveEducation refuses rows whose variant is not the namespaced edu-
// prefix, preserving the record, activation and file.
func TestRemoveEducationRefusesUnexpectedVariant(t *testing.T) {
	dir, db, s, m, _ := eduServices(t)
	id := "dddddddddddddddddddddddddddddddd"
	body := []byte("education zim bytes variant")
	eduSeed(t, dir, db, m, id, "afet-ilk-yardim", "education", body)
	if _, _, err := s.Install(m, id); err != nil {
		t.Fatal(err)
	}
	installed := waitPkg(t, s, id, "installed")
	if _, err := db.Exec(`UPDATE content_packages SET variant='nopic' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveEducation(id); !isPkgCode(err, "invalid_request") {
		t.Fatalf("unexpected variant must be refused, got %v", err)
	}
	after, err := s.get(id)
	if err != nil || after.Status != "installed" {
		t.Fatalf("record disturbed %+v %v", after, err)
	}
	if _, err := os.Stat(installed.LocalPath); err != nil {
		t.Fatalf("file disturbed: %v", err)
	}
	if files, err := s.InstalledSnapshot(); err != nil || len(files) != 1 {
		t.Fatalf("snapshot disturbed %+v %v", files, err)
	}
}

// A symlinked install target is refused without deletion.
func TestRemoveEducationRefusesSymlinkTarget(t *testing.T) {
	dir, db, s, m, _ := eduServices(t)
	id := "cccccccccccccccccccccccccccccccc"
	body := []byte("education zim bytes symlink")
	eduSeed(t, dir, db, m, id, "saglik", "education", body)
	if _, _, err := s.Install(m, id); err != nil {
		t.Fatal(err)
	}
	installed := waitPkg(t, s, id, "installed")
	target := filepath.Join(dir, "content", "kiwix", id+".zim")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(installed.SourcePath, target); err != nil {
		t.Skip(err)
	}
	if err := s.RemoveEducation(id); !isPkgCode(err, "ownership_conflict") {
		t.Fatalf("symlink removal must be refused, got %v", err)
	}
	if _, err := s.get(id); err != nil {
		t.Fatalf("record must survive refused removal: %v", err)
	}
	link, err := os.Lstat(target)
	if err != nil || link.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink disturbed: %v %v", link, err)
	}
}

// TestRemoveEducationReentrantCallback fires the installed notification from
// a callback that calls back into the service: the notification must run
// outside the service lock (repository.installed pattern), so this completes
// instead of deadlocking.
func TestRemoveEducationReentrantCallback(t *testing.T) {
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := download.New(db, filepath.Join(dir, "downloads"), download.Options{})
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	var s *Service
	done := make(chan struct{}, 1)
	s, err = New(context.Background(), db, dir, Options{OnInstalled: func() {
		_, _ = s.List("")
		done <- struct{}{}
	}})
	if err != nil {
		m.Close()
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(); m.Close(); db.Close() })
	id := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	body := []byte("education zim bytes callback")
	sum := sha256.Sum256(body)
	stamp := now()
	path := filepath.Join(dir, "downloads", id+".part")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO downloads(id,provider,provider_package_id,title,variant,file_name,source_url,temp_path,total_bytes,downloaded_bytes,status,created_at,updated_at,content_type,language,version,expected_size,checksum_algorithm,checksum_value) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, "kiwix", "afet-ilk-yardim", "Edu", "edu-afet-ilk-yardim", "edu.zim", "https://upstream.invalid/private", path, len(body), len(body), "completed", stamp, stamp, "education", "tr", "2026-10-01", len(body), "sha256", hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Install(m, id); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r, err := s.get(id)
		if err == nil && r.Status == "installed" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	// Drain the install-time notification.
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("install notification missing")
	}
	// installed state and its notification precede the install worker's
	// settlement. RemoveEducation correctly refuses a still-active worker;
	// wait for the real completion signal rather than racing its cleanup.
	s.mu.Lock()
	for s.active[id] {
		s.settled.Wait()
	}
	s.mu.Unlock()
	errCh := make(chan error, 1)
	go func() { errCh <- s.RemoveEducation(id) }()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("remove: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RemoveEducation deadlocked under re-entrant callback")
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("remove notification missing")
	}
}
