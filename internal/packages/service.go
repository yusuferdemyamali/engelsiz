package packages

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"engelsiz/internal/download"
)

// Options exposes fault boundaries for deterministic lifecycle tests. Hook is
// called outside the transfer manager's operation lock.
type Options struct {
	Logger      *log.Logger
	Hook        func(context.Context, string) error
	OnInstalled func() // notification only; called after durable commit, outside locks
}
type Service struct {
	db                   *sql.DB
	dataDir              string
	root, source, target *os.Root
	ctx                  context.Context
	cancel               context.CancelFunc
	mu                   sync.Mutex
	wg                   sync.WaitGroup
	active               map[string]bool
	accepted             map[string]os.FileInfo
	closed               bool
	persistErr           error
	opts                 Options
}

func New(ctx context.Context, db *sql.DB, dataDir string, opts Options) (*Service, error) {
	absolute, e := filepath.Abs(dataDir)
	if e != nil {
		return nil, e
	}
	info, e := os.Lstat(absolute)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrUnsafe
	}
	root, e := os.OpenRoot(absolute)
	if e != nil {
		return nil, e
	}
	work, cancel := context.WithCancel(ctx)
	s := &Service{db: db, dataDir: absolute, root: root, ctx: work, cancel: cancel, active: map[string]bool{}, accepted: map[string]os.FileInfo{}, opts: opts}
	ok := false
	defer func() {
		if !ok {
			s.Close()
		}
	}()
	for _, name := range []string{"downloads", "content", "content/kiwix"} {
		e = root.Mkdir(name, 0700)
		if e != nil && !errors.Is(e, os.ErrExist) {
			return nil, e
		}
		info, e = root.Lstat(name)
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, ErrUnsafe
		}
	}
	s.source, e = root.OpenRoot("downloads")
	if e != nil {
		return nil, e
	}
	s.target, e = root.OpenRoot("content/kiwix")
	if e != nil {
		return nil, e
	}
	if e = s.recoverUpdates(); e != nil {
		return nil, e
	}
	// Reconcile accepted local work before download.New performs ordinary
	// transfer recovery. This operation never fetches upstream metadata.
	rows, e := s.records()
	if e != nil {
		return nil, e
	}
	for _, r := range rows {
		if r.Status == "failed" {
			continue
		}
		if e = s.run(&r, true); e != nil {
			return nil, e
		}
	}
	if e = s.recoverUpdates(); e != nil {
		return nil, e
	}
	ok = true
	return s, nil
}
func (s *Service) List(status string) ([]Package, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.persistErr != nil {
		return nil, download.ErrStorage
	}
	rows, e := s.records()
	if e != nil {
		return nil, download.ErrStorage
	}
	out := []Package{}
	for _, r := range rows {
		if status == "" || r.Status == status {
			var selected int
			if e = s.db.QueryRow(`SELECT COUNT(*) FROM active_packages WHERE package_id=?`, r.ID).Scan(&selected); e != nil {
				return nil, download.ErrStorage
			}
			r.Active = selected != 0 && r.Status == "installed"
			out = append(out, r.Package)
		}
	}
	return out, nil
}
func validMetadata(a download.Artifact) error {
	if a.Provider != "kiwix" || a.ProviderPackageID == "" || a.ContentType == "" || a.Language == "" || a.Variant == "" || a.Title == "" || a.Version == "" || a.ExpectedSize == nil || *a.ExpectedSize <= 0 {
		return ErrMetadata
	}
	b, e := hex.DecodeString(a.ChecksumValue)
	if a.ChecksumAlgorithm != "sha256" || e != nil || len(b) != sha256.Size {
		return ErrChecksum
	}
	return nil
}
func (s *Service) Install(m *download.Manager, id string) (Package, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.ctx.Err() != nil || s.persistErr != nil {
		return Package{}, false, download.ErrStorage
	}
	if !s.directoriesOwned() {
		return Package{}, false, ErrUnsafe
	}
	r, e := s.get(id)
	if e == nil {
		if r.Status == "installed" {
			return r.Package, true, nil
		}
		if s.active[id] {
			return r.Package, false, nil
		}
	}
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return Package{}, false, download.ErrStorage
	}
	a, e := m.ClaimCompleted(id, id)
	if e != nil {
		return Package{}, false, e
	}
	if e = validMetadata(a); e != nil {
		return Package{}, false, e
	}
	destination := filepath.Join(s.dataDir, "content", "kiwix", id+".zim")
	if _, e = s.target.Lstat(id + ".zim"); e == nil {
		return Package{}, false, ErrCollision
	} else if !errors.Is(e, os.ErrNotExist) {
		return Package{}, false, ErrUnsafe
	}
	var n int
	if e = s.db.QueryRow(`SELECT COUNT(*) FROM content_packages WHERE provider=? AND provider_package_id=? AND version=? AND status IN ('verifying','installing','installed') AND id!=?`, a.Provider, a.ProviderPackageID, a.Version, id).Scan(&n); e != nil {
		return Package{}, false, download.ErrStorage
	}
	if n != 0 {
		return Package{}, false, ErrCollision
	}
	if r.ID == "" {
		stamp := now()
		r = record{Package: Package{ID: id, DownloadID: id, Provider: a.Provider, ProviderPackageID: a.ProviderPackageID, ContentType: a.ContentType, Language: a.Language, Variant: a.Variant, Title: a.Title, Version: a.Version, SizeBytes: *a.ExpectedSize, Checksum: Checksum{Algorithm: a.ChecksumAlgorithm, Value: strings.ToLower(a.ChecksumValue)}, Status: "verifying", CreatedAt: stamp, UpdatedAt: stamp}, SourceURL: a.SourceURL, SourcePath: a.TempPath, LocalPath: destination}
		if e = s.insert(&r); e != nil {
			return Package{}, false, download.ErrStorage
		}
	} else {
		r.Status = "verifying"
		r.Error = nil
		r.InstalledAt = nil
		if e = s.save(&r); e != nil {
			return Package{}, false, download.ErrStorage
		}
	}
	snapshot := r.Package
	s.active[id] = true
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		e := s.run(&r, false)
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.active, id)
		if e != nil && !errors.Is(e, context.Canceled) {
			s.persistErr = e
			if s.opts.Logger != nil {
				s.opts.Logger.Printf("package %s persistence: %v", id, e)
			}
		}
	}()
	return snapshot, false, nil
}
func (s *Service) hook(stage string) error {
	if e := s.ctx.Err(); e != nil {
		return e
	}
	if s.opts.Hook != nil {
		return s.opts.Hook(s.ctx, stage)
	}
	return nil
}
func (s *Service) fail(r *record, problem *download.Error) error {
	if s.opts.Logger != nil {
		s.opts.Logger.Printf("package %s failed: %s", r.ID, problem.Code)
	}
	r.Status = "failed"
	r.Error = problem
	r.InstalledAt = nil
	if err := s.save(r); err != nil {
		return err
	}
	_, err := s.db.Exec(`UPDATE package_updates SET state='failed',error_code=?,error_message=?,updated_at=? WHERE download_id=? AND state IN ('creating','downloading')`, problem.Code, problem.Message, now(), r.ID)
	return err
}
func (s *Service) owned(r *record) bool {
	id, e := hex.DecodeString(r.ID)
	return e == nil && len(id) == 16 && r.DownloadID == r.ID && r.SourcePath == filepath.Join(s.dataDir, "downloads", r.ID+".part") && r.LocalPath == filepath.Join(s.dataDir, "content", "kiwix", r.ID+".zim") && r.Checksum.Algorithm == "sha256" && r.SizeBytes > 0
}
func (s *Service) presence(root *os.Root, name string) (bool, error) {
	i, e := root.Lstat(name)
	if errors.Is(e, os.ErrNotExist) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	if !i.Mode().IsRegular() {
		return true, ErrUnsafe
	}
	return true, nil
}
func (s *Service) verify(root *os.Root, name string, r *record) (os.FileInfo, error) {
	before, e := root.Lstat(name)
	if e != nil {
		return nil, e
	}
	if !before.Mode().IsRegular() {
		return nil, ErrUnsafe
	}
	f, e := root.Open(name)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !os.SameFile(before, info) || info.Size() != r.SizeBytes {
		return nil, ErrVerification
	}
	hash := sha256.New()
	buf := make([]byte, 256*1024)
	var count int64
	for {
		if e = s.ctx.Err(); e != nil {
			return nil, e
		}
		n, readErr := f.Read(buf)
		if n > 0 {
			count += int64(n)
			hash.Write(buf[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	after, e := root.Lstat(name)
	if e != nil {
		return nil, e
	}
	if !after.Mode().IsRegular() || !os.SameFile(info, after) || after.Size() != count || !after.ModTime().Equal(info.ModTime()) || count != r.SizeBytes || hex.EncodeToString(hash.Sum(nil)) != r.Checksum.Value {
		return nil, ErrVerification
	}
	return after, nil
}
func (s *Service) directoriesOwned() bool {
	currentRoot, e := os.Lstat(s.dataDir)
	if e != nil || !currentRoot.IsDir() || currentRoot.Mode()&os.ModeSymlink != 0 {
		return false
	}
	originalRoot, e := s.root.Stat(".")
	if e != nil || !os.SameFile(currentRoot, originalRoot) {
		return false
	}
	for name, pinned := range map[string]*os.Root{"downloads": s.source, "content/kiwix": s.target} {
		current, e := s.root.Lstat(name)
		if e != nil || !current.IsDir() || current.Mode()&os.ModeSymlink != 0 {
			return false
		}
		original, e := pinned.Stat(".")
		if e != nil || !os.SameFile(current, original) {
			return false
		}
	}
	parent, e := s.root.Lstat("content")
	return e == nil && parent.IsDir() && parent.Mode()&os.ModeSymlink == 0
}
func (s *Service) run(r *record, recovery bool) error {
	if !s.directoriesOwned() {
		return s.fail(r, ErrUnsafe)
	}
	if !s.owned(r) {
		return s.fail(r, ErrUnsafe)
	}
	var owner, state, status string
	if e := s.db.QueryRow(`SELECT COALESCE(artifact_owner,''),artifact_state,status FROM downloads WHERE id=?`, r.DownloadID).Scan(&owner, &state, &status); e != nil {
		return e
	}
	if owner != r.ID || status != "completed" || (state != "claimed" && state != "consumed") {
		return s.fail(r, ErrUnsafe)
	}
	src, dst := r.ID+".part", r.ID+".zim"
	source, e := s.presence(s.source, src)
	if e != nil {
		return s.fail(r, ErrUnsafe)
	}
	target, e := s.presence(s.target, dst)
	if e != nil {
		return s.fail(r, ErrUnsafe)
	}
	if source && target {
		return s.fail(r, ErrFiles)
	}
	if target {
		if !recovery || (r.Status != "installing" && r.Status != "installed") {
			return s.fail(r, ErrCollision)
		}
		verified, verifyErr := s.verify(s.target, dst, r)
		e = verifyErr
		if e != nil {
			if errors.Is(e, context.Canceled) {
				return e
			}
			return s.fail(r, ErrVerification)
		}
		return s.installed(r, verified)
	}
	if !source || r.Status == "installed" {
		return s.fail(r, ErrFiles)
	}
	if e = s.hook("verify"); e != nil {
		return e
	}
	verified, e := s.verify(s.source, src, r)
	if e != nil {
		if errors.Is(e, context.Canceled) {
			return e
		}
		return s.fail(r, ErrVerification)
	}
	if e = s.hook("intent"); e != nil {
		return e
	}
	r.Status = "installing"
	r.Error = nil
	if e = s.save(r); e != nil {
		return e
	}
	if e = s.hook("rename"); e != nil {
		return e
	}
	current, e := s.source.Lstat(src)
	if e != nil || !current.Mode().IsRegular() || !os.SameFile(verified, current) || current.Size() != verified.Size() || !current.ModTime().Equal(verified.ModTime()) {
		return s.fail(r, ErrUnsafe)
	}
	if e = renameNoReplace(s.source, src, s.target, dst); e != nil {
		if errors.Is(e, os.ErrExist) {
			return s.fail(r, ErrCollision)
		}
		return s.fail(r, download.ErrStorage)
	}
	// Check the actual published inode as well; a source replaced immediately
	// around rename must never become trusted installed content.
	published, verifyErr := s.verify(s.target, dst, r)
	e = verifyErr
	if e != nil {
		if errors.Is(e, context.Canceled) {
			return e
		}
		return s.fail(r, ErrVerification)
	}
	if e = s.hook("commit"); e != nil {
		return e
	}
	return s.installed(r, published)
}
func (s *Service) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
	var e error
	for _, root := range []*os.Root{s.target, s.source, s.root} {
		if root != nil {
			e = errors.Join(e, root.Close())
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return errors.Join(e, s.persistErr)
}
