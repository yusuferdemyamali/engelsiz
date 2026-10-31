package packages

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"engelsiz/internal/database"
	"engelsiz/internal/download"
)

const testID = "0123456789abcdef0123456789abcdef"

type fixture struct {
	dir  string
	db   *sql.DB
	s    *Service
	m    *download.Manager
	body []byte
}

func newFixture(t *testing.T, opts Options) *fixture {
	t.Helper()
	dir := t.TempDir()
	db, e := database.Open(filepath.Join(dir, "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	s, e := New(context.Background(), db, dir, opts)
	if e != nil {
		t.Fatal(e)
	}
	m, e := download.New(db, filepath.Join(dir, "downloads"), download.Options{})
	if e != nil {
		t.Fatal(e)
	}
	f := &fixture{dir: dir, db: db, s: s, m: m, body: []byte("owned immutable ZIM bytes")}
	t.Cleanup(func() { s.Close(); m.Close(); db.Close() })
	f.seed(t, testID, "release", f.body)
	return f
}
func (f *fixture) seed(t *testing.T, id, pkg string, body []byte) {
	t.Helper()
	sum := sha256.Sum256(body)
	stamp := now()
	path := filepath.Join(f.dir, "downloads", id+".part")
	if e := os.WriteFile(path, body, 0600); e != nil {
		t.Fatal(e)
	}
	_, e := f.db.Exec(`INSERT INTO downloads(id,provider,provider_package_id,title,variant,file_name,source_url,temp_path,total_bytes,downloaded_bytes,status,created_at,updated_at,content_type,language,version,expected_size,checksum_algorithm,checksum_value) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, "kiwix", pkg, "Türkçe Wikipedia", "nopic", "selected.zim", "https://upstream.invalid/private", path, len(body), len(body), "completed", stamp, stamp, "wikipedia", "tr", "2026-10-01", len(body), "sha256", hex.EncodeToString(sum[:]))
	if e != nil {
		t.Fatal(e)
	}
}
func (f *fixture) wait(t *testing.T, status string) record {
	t.Helper()
	limit := time.Now().Add(5 * time.Second)
	for time.Now().Before(limit) {
		r, e := f.s.get(testID)
		if e == nil && r.Status == status {
			return r
		}
		time.Sleep(time.Millisecond)
	}
	r, e := f.s.get(testID)
	t.Fatalf("wanted %s got %+v %v", status, r, e)
	return record{}
}
func TestInstallSuccessStableIdentityAndCompletedHistory(t *testing.T) {
	f := newFixture(t, Options{})
	p, installed, e := f.s.Install(f.m, testID)
	if e != nil || installed || p.Status != "verifying" {
		t.Fatalf("accept %+v %v %v", p, installed, e)
	}
	r := f.wait(t, "installed")
	if r.InstalledAt == nil || r.Error != nil || r.Version != "2026-10-01" || r.Variant != "nopic" {
		t.Fatalf("registry %+v", r)
	}
	p, installed, e = f.s.Install(f.m, testID)
	if e != nil || !installed || p.ID != testID {
		t.Fatalf("repeat %+v %v %v", p, installed, e)
	}
	body, e := os.ReadFile(r.LocalPath)
	if e != nil || !bytes.Equal(body, f.body) {
		t.Fatalf("final %q %v", body, e)
	}
	if _, e = os.Stat(r.SourcePath); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("source retained %v", e)
	}
	f.s.Close()
	f.m.Close()
	f.db.Close()
	db, e := database.Open(filepath.Join(f.dir, "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s, e := New(context.Background(), db, f.dir, Options{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	m, e := download.New(db, filepath.Join(f.dir, "downloads"), download.Options{})
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	items, e := s.List("installed")
	if e != nil || len(items) != 1 || items[0].InstalledAt == nil || *items[0].InstalledAt != *r.InstalledAt {
		t.Fatalf("reopen %+v %v", items, e)
	}
	transfer, e := m.Get(testID)
	if e != nil || transfer.Status != "completed" || transfer.DownloadedBytes != int64(len(f.body)) {
		t.Fatalf("completed history %+v %v", transfer, e)
	}
}
func TestVerificationFailuresAndExplicitRetry(t *testing.T) {
	for _, field := range []string{"checksum", "size"} {
		t.Run(field, func(t *testing.T) {
			f := newFixture(t, Options{})
			if field == "checksum" {
				f.db.Exec(`UPDATE downloads SET checksum_value=? WHERE id=?`, strings.Repeat("0", 64), testID)
			} else {
				f.db.Exec(`UPDATE downloads SET expected_size=expected_size+1 WHERE id=?`, testID)
			}
			p, _, e := f.s.Install(f.m, testID)
			if e != nil {
				t.Fatal(e)
			}
			r := f.wait(t, "failed")
			if r.Error == nil || r.Error.Code != "verification_failed" || r.InstalledAt != nil {
				t.Fatalf("failed %+v", r)
			}
			if _, e = os.Stat(r.LocalPath); !errors.Is(e, os.ErrNotExist) {
				t.Fatalf("bad final %v", e)
			}
			if _, e = os.Stat(r.SourcePath); e != nil {
				t.Fatal(e)
			}
			retry, _, e := f.s.Install(f.m, testID)
			if e != nil || retry.ID != p.ID {
				t.Fatalf("retry %+v %v", retry, e)
			}
			f.wait(t, "failed")
			var n int
			f.db.QueryRow(`SELECT COUNT(*) FROM content_packages`).Scan(&n)
			if n != 1 {
				t.Fatal("duplicate retry rows")
			}
		})
	}
}
func TestUnsafeSourcesCollisionsAndMetadata(t *testing.T) {
	for _, scenario := range []string{"symlink", "directory", "path", "target", "target-symlink", "owner", "legacy", "unsupported", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t, Options{})
			src := filepath.Join(f.dir, "downloads", testID+".part")
			target := filepath.Join(f.dir, "content", "kiwix", testID+".zim")
			var want error = ErrUnsafe
			switch scenario {
			case "symlink":
				os.Remove(src)
				if e := os.Symlink(filepath.Join(f.dir, "outside"), src); e != nil {
					t.Skip(e)
				}
			case "directory":
				os.Remove(src)
				os.Mkdir(src, 0700)
			case "path":
				f.db.Exec(`UPDATE downloads SET temp_path=? WHERE id=?`, filepath.Join(f.dir, "foreign"), testID)
			case "target":
				os.WriteFile(target, []byte("foreign"), 0600)
				want = ErrCollision
			case "target-symlink":
				if e := os.Symlink(src, target); e != nil {
					t.Skip(e)
				}
				want = ErrCollision
			case "owner":
				f.db.Exec(`UPDATE downloads SET artifact_owner='foreign',artifact_state='claimed' WHERE id=?`, testID)
			case "legacy":
				f.db.Exec(`UPDATE downloads SET version=NULL,language=NULL WHERE id=?`, testID)
				want = ErrMetadata
			case "unsupported":
				f.db.Exec(`UPDATE downloads SET checksum_algorithm='sha1' WHERE id=?`, testID)
				want = ErrChecksum
			case "missing":
				os.Remove(src)
			}
			_, _, e := f.s.Install(f.m, testID)
			var problem *download.Error
			if !errors.As(e, &problem) || problem.Code != want.(*download.Error).Code {
				t.Fatalf("got %v want %v", e, want)
			}
			items, e := f.s.List("")
			if e != nil || len(items) != 0 {
				t.Fatalf("unsafe published %+v %v", items, e)
			}
			if scenario == "target" {
				b, _ := os.ReadFile(target)
				if string(b) != "foreign" {
					t.Fatal("overwrote target")
				}
			}
		})
	}
}
func TestVerificationDoesNotHoldTransferControlLockAndDuplicateWorkers(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	f := newFixture(t, Options{Hook: func(ctx context.Context, stage string) error {
		if stage == "verify" {
			once.Do(func() { close(entered) })
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}})
	first, _, e := f.s.Install(f.m, testID)
	if e != nil {
		t.Fatal(e)
	}
	<-entered
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, installed, e := f.s.Install(f.m, testID)
			if e != nil || installed || p.ID != first.ID {
				t.Errorf("duplicate %+v %v %v", p, installed, e)
			}
		}()
	}
	wg.Wait()
	// A separate paused transfer can be controlled while verification is blocked.
	other := "1123456789abcdef0123456789abcdef"
	f.seed(t, other, "other", []byte("partial"))
	f.db.Exec(`UPDATE downloads SET status='paused' WHERE id=?`, other)
	done := make(chan error, 1)
	go func() { _, e := f.m.Control(other, "cancel"); done <- e }()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("verification blocked transfer controls")
	}
	close(release)
	f.wait(t, "installed")
}
func TestCrashBoundariesReconcileBeforeTransferRecovery(t *testing.T) {
	for _, stage := range []string{"verify", "intent", "rename", "commit"} {
		t.Run(stage, func(t *testing.T) {
			reached := make(chan struct{})
			f := newFixture(t, Options{Hook: func(ctx context.Context, current string) error {
				if current == stage {
					close(reached)
					<-ctx.Done()
					return ctx.Err()
				}
				return nil
			}})
			if _, _, e := f.s.Install(f.m, testID); e != nil {
				t.Fatal(e)
			}
			<-reached
			if e := f.s.Close(); e != nil {
				t.Fatal(e)
			}
			f.m.Close()
			f.db.Close()
			db, e := database.Open(filepath.Join(f.dir, "state.db"))
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			s, e := New(context.Background(), db, f.dir, Options{})
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			m, e := download.New(db, filepath.Join(f.dir, "downloads"), download.Options{})
			if e != nil {
				t.Fatal(e)
			}
			defer m.Close()
			items, e := s.List("installed")
			if e != nil || len(items) != 1 {
				t.Fatalf("recovery %+v %v", items, e)
			}
			d, e := m.Get(testID)
			if e != nil || d.Status != "completed" {
				t.Fatalf("transfer recovery %+v %v", d, e)
			}
		})
	}
}
func TestRenameCollisionAfterAcceptancePreservesBothFiles(t *testing.T) {
	f := newFixture(t, Options{})
	// Replace hook only before any worker exists.
	f.s.opts.Hook = func(ctx context.Context, stage string) error {
		if stage == "rename" {
			return os.WriteFile(filepath.Join(f.dir, "content", "kiwix", testID+".zim"), []byte("foreign"), 0600)
		}
		return nil
	}
	if _, _, e := f.s.Install(f.m, testID); e != nil {
		t.Fatal(e)
	}
	r := f.wait(t, "failed")
	if r.Error == nil || r.Error.Code != "target_collision" {
		t.Fatalf("late collision %+v", r)
	}
	b, _ := os.ReadFile(r.LocalPath)
	if string(b) != "foreign" {
		t.Fatal("foreign overwritten")
	}
	if _, e := os.Stat(r.SourcePath); e != nil {
		t.Fatal("source lost")
	}
}
func TestRecoveryMissingAmbiguousAndInvalidInstalledFiles(t *testing.T) {
	for _, mode := range []string{"missing", "both", "corrupt", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, Options{})
			if _, _, e := f.s.Install(f.m, testID); e != nil {
				t.Fatal(e)
			}
			r := f.wait(t, "installed")
			f.s.Close()
			f.m.Close()
			switch mode {
			case "missing":
				os.Remove(r.LocalPath)
			case "both":
				os.WriteFile(r.SourcePath, f.body, 0600)
			case "corrupt":
				os.WriteFile(r.LocalPath, []byte("bad"), 0600)
			case "symlink":
				os.Remove(r.LocalPath)
				if e := os.Symlink(r.SourcePath, r.LocalPath); e != nil {
					t.Skip(e)
				}
			}
			s, e := New(context.Background(), f.db, f.dir, Options{})
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			items, e := s.List("installed")
			if e != nil || len(items) != 0 {
				t.Fatalf("bad installed %+v %v", items, e)
			}
			rows, _ := s.List("")
			if len(rows) != 1 || rows[0].Status != "failed" || rows[0].InstalledAt != nil {
				t.Fatalf("failed recovery %+v", rows)
			}
		})
	}
}
func TestPersistenceFailuresRetainRecoverableIntent(t *testing.T) {
	for _, stage := range []string{"intent", "commit"} {
		t.Run(stage, func(t *testing.T) {
			f := newFixture(t, Options{})
			f.s.opts.Hook = func(ctx context.Context, current string) error {
				if current == stage {
					_, e := f.db.Exec(`CREATE TRIGGER reject_package_update BEFORE UPDATE ON content_packages BEGIN SELECT RAISE(ABORT,'injected private storage failure'); END;`)
					return e
				}
				return nil
			}
			if _, _, e := f.s.Install(f.m, testID); e != nil {
				t.Fatal(e)
			}
			limit := time.Now().Add(3 * time.Second)
			for time.Now().Before(limit) {
				f.s.mu.Lock()
				e := f.s.persistErr
				f.s.mu.Unlock()
				if e != nil {
					break
				}
				time.Sleep(time.Millisecond)
			}
			if _, e := f.s.List(""); e == nil {
				t.Fatal("persistence failure not surfaced")
			}
			f.s.Close()
			f.m.Close()
			f.db.Exec(`DROP TRIGGER reject_package_update`)
			s, e := New(context.Background(), f.db, f.dir, Options{})
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			rows, e := s.List("installed")
			if e != nil || len(rows) != 1 {
				t.Fatalf("DB failure recovery %+v %v", rows, e)
			}
		})
	}
}

func TestRenamePermissionFailureRetainsSourceForRetry(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	f := newFixture(t, Options{})
	targetDir := filepath.Join(f.dir, "content", "kiwix")
	defer os.Chmod(targetDir, 0700)
	f.s.opts.Hook = func(ctx context.Context, stage string) error {
		if stage == "rename" {
			return os.Chmod(targetDir, 0500)
		}
		return nil
	}
	if _, _, e := f.s.Install(f.m, testID); e != nil {
		t.Fatal(e)
	}
	r := f.wait(t, "failed")
	if r.Error == nil || r.Error.Code != "storage_error" || r.InstalledAt != nil {
		t.Fatalf("rename failure %+v", r)
	}
	if _, e := os.Stat(r.SourcePath); e != nil {
		t.Fatalf("source lost: %v", e)
	}
	if _, e := os.Stat(r.LocalPath); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("unexpected final: %v", e)
	}
	// Wait for worker exit before changing a test hook, just as lifecycle shutdown
	// joins workers before changing their resources.
	f.s.mu.Lock()
	active := f.s.active[testID]
	f.s.mu.Unlock()
	for active {
		time.Sleep(time.Millisecond)
		f.s.mu.Lock()
		active = f.s.active[testID]
		f.s.mu.Unlock()
	}
	f.s.opts.Hook = nil
	if e := os.Chmod(targetDir, 0700); e != nil {
		t.Fatal(e)
	}
	if _, _, e := f.s.Install(f.m, testID); e != nil {
		t.Fatal(e)
	}
	f.wait(t, "installed")
}

func TestDirectoryReplacementCannotPublishInstalledPath(t *testing.T) {
	f := newFixture(t, Options{})
	f.s.opts.Hook = func(ctx context.Context, stage string) error {
		if stage == "rename" {
			path := filepath.Join(f.dir, "content", "kiwix")
			if e := os.Rename(path, path+"-original"); e != nil {
				return e
			}
			return os.Mkdir(path, 0700)
		}
		return nil
	}
	if _, _, e := f.s.Install(f.m, testID); e != nil {
		t.Fatal(e)
	}
	r := f.wait(t, "failed")
	if r.Error == nil || r.Error.Code != "ownership_conflict" || r.InstalledAt != nil {
		t.Fatalf("directory replacement %+v", r)
	}
	if _, e := os.Stat(r.LocalPath); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("foreign path published %v", e)
	}
}

func TestDataRootReplacementCannotPublishInstalledPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows prevents renaming a data root containing the open SQLite database; content-directory replacement is tested separately")
	}
	f := newFixture(t, Options{})
	moved := f.dir + "-original"
	defer os.RemoveAll(moved)
	f.s.opts.Hook = func(ctx context.Context, stage string) error {
		if stage == "rename" {
			if e := os.Rename(f.dir, moved); e != nil {
				return e
			}
			return os.MkdirAll(filepath.Join(f.dir, "content", "kiwix"), 0700)
		}
		return nil
	}
	if _, _, e := f.s.Install(f.m, testID); e != nil {
		t.Fatal(e)
	}
	r := f.wait(t, "failed")
	if r.Error == nil || r.Error.Code != "ownership_conflict" || r.InstalledAt != nil {
		t.Fatalf("root replacement %+v", r)
	}
	if _, e := os.Stat(r.LocalPath); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("foreign absolute path published %v", e)
	}
	if _, e := os.Stat(filepath.Join(moved, "content", "kiwix", testID+".zim")); e != nil {
		t.Fatalf("original evidence lost %v", e)
	}
}

func TestFreshCorrectDownloadCanReplaceFailedReleaseReservation(t *testing.T) {
	f := newFixture(t, Options{})
	// A transfer can complete with corrupt bytes. Immutable trusted metadata
	// stays unchanged; installation must reject those bytes and preserve them.
	bad := bytes.Repeat([]byte("x"), len(f.body))
	source := filepath.Join(f.dir, "downloads", testID+".part")
	if e := os.WriteFile(source, bad, 0600); e != nil {
		t.Fatal(e)
	}
	if _, _, e := f.s.Install(f.m, testID); e != nil {
		t.Fatal(e)
	}
	failed := f.wait(t, "failed")
	if failed.Error == nil || failed.Error.Code != "verification_failed" {
		t.Fatalf("first artifact %+v", failed)
	}
	freshID := "2123456789abcdef0123456789abcdef"
	f.seed(t, freshID, "release", f.body)
	fresh, _, e := f.s.Install(f.m, freshID)
	if e != nil {
		t.Fatalf("fresh acquisition blocked by failed release: %v", e)
	}
	deadline := time.Now().Add(3 * time.Second)
	var installed record
	for time.Now().Before(deadline) {
		installed, e = f.s.get(freshID)
		if e == nil && installed.Status == "installed" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if installed.Status != "installed" || installed.ID != fresh.ID {
		t.Fatalf("fresh install %+v %v", installed, e)
	}
	old, e := f.s.get(testID)
	if e != nil || old.Status != "failed" || old.ID != failed.ID {
		t.Fatalf("failed history changed %+v %v", old, e)
	}
	preserved, e := os.ReadFile(source)
	if e != nil || !bytes.Equal(preserved, bad) {
		t.Fatalf("failed evidence changed %v", e)
	}
	body, e := os.ReadFile(installed.LocalPath)
	if e != nil || !bytes.Equal(body, f.body) {
		t.Fatalf("fresh final %q %v", body, e)
	}
	if _, _, e = f.s.Install(f.m, testID); !errors.Is(e, ErrCollision) {
		t.Fatalf("old retry must not reserve installed release: %v", e)
	}
	rows, e := f.s.List("installed")
	if e != nil || len(rows) != 1 || rows[0].ID != freshID {
		t.Fatalf("release duplicated %+v %v", rows, e)
	}
}
