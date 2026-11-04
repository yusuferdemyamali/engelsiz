package translation

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"engelsiz/internal/download"
)

// Options configures the translation service.
//
// Pins carries verified per-model acquisition metadata. A nil map selects the
// catalog DefaultPins; an explicitly empty map disables all models. Pins are
// the only source of download URLs, sizes and checksums.
type Options struct {
	Pins             map[string]Acquisition
	Binary           string
	TranslateTimeout time.Duration
	Logger           *log.Logger
}

// Service owns the durable translation registry, model installation and the
// persistent worker process. Missing engine binary never fails New; translate
// then reports engine_unavailable while the rest of the app keeps running.
type Service struct {
	db           *sql.DB
	dataRoot     string
	dir          string
	downloadsDir string
	root         *os.Root // dataRoot handle (ownership checks)
	models       *os.Root // models dir handle (confined checks)
	downloads    *os.Root // downloads dir handle (single-open artifacts)
	pins         map[string]Acquisition
	binary       string
	timeout      time.Duration
	logger       *log.Logger

	mu     sync.Mutex
	active map[string]bool
	closed bool

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	procMu sync.Mutex
	proc   *workerProc
}

// Model is the public per-direction snapshot.
type Model struct {
	ID             string             `json:"id"`
	SourceLanguage string             `json:"source_language"`
	TargetLanguage string             `json:"target_language"`
	Version        string             `json:"version"`
	SizeBytes      int64              `json:"size_bytes"`
	Status         string             `json:"status"`
	Download       *download.Snapshot `json:"download"`
	Error          *download.Error    `json:"error"`
}

// Status is the GET /api/translation response body.
type Status struct {
	Models           []Model `json:"models"`
	RuntimeAvailable bool    `json:"runtime_available"`
	MaxCharacters    int     `json:"max_characters"`
}

type row struct {
	DownloadID string
	Version    string
	Status     string
	Size       sql.NullInt64
	Error      *download.Error
	present    bool
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func (s *Service) logf(format string, args ...any) {
	if s.logger != nil {
		s.logger.Printf("translation "+format, args...)
	}
}

// mergeDone returns a context cancelled when either the caller or the service
// shuts down, bounding queue waits and in-flight work by service lifetime.
func mergeDone(ctx context.Context, svcCtx context.Context) (context.Context, context.CancelFunc) {
	merged, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-svcCtx.Done():
			cancel()
		case <-merged.Done():
		}
	}()
	return merged, cancel
}

func New(db *sql.DB, dataRoot string, opts Options) (*Service, error) {
	absolute, err := filepath.Abs(dataRoot)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrUnsafe
	}
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			root.Close()
		}
	}()
	for _, name := range []string{"content", "content/translation", "downloads"} {
		if err := root.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if fi, err := root.Lstat(name); err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return nil, ErrUnsafe
		}
	}
	models, err := root.OpenRoot("content/translation")
	if err != nil {
		return nil, err
	}
	downloads, err := root.OpenRoot("downloads")
	if err != nil {
		models.Close()
		return nil, err
	}
	defer func() {
		if !ok {
			models.Close()
			downloads.Close()
		}
	}()
	timeout := opts.TranslateTimeout
	if timeout <= 0 {
		timeout = defaultTranslateTimeout
	}
	if opts.Pins == nil {
		opts.Pins = DefaultPins
	}
	pins := make(map[string]Acquisition, len(opts.Pins))
	for id, pin := range opts.Pins {
		pins[id] = pin
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{
		db: db, dataRoot: absolute, dir: filepath.Join(absolute, "content", "translation"),
		downloadsDir: filepath.Join(absolute, "downloads"),
		root:         root, models: models, downloads: downloads, pins: pins, binary: resolveBinary(opts.Binary),
		timeout: timeout, logger: opts.Logger, active: map[string]bool{},
		ctx: ctx, cancel: cancel,
	}
	if err := s.recover(); err != nil {
		cancel()
		return nil, err
	}
	ok = true
	return s, nil
}

// getRow reads the durable registry. A missing row is empty and valid; a
// database failure is a separate error so stored per-model errors can never
// be confused with broken storage (one model's failure must not disable the
// other direction).
func (s *Service) getRow(id string) (row, error) {
	var r row
	var size sql.NullInt64
	var code, message, downloadID, version, status sql.NullString
	err := s.db.QueryRow(`SELECT download_id,version,status,size_bytes,error_code,error_message FROM translation_models WHERE id=?`, id).
		Scan(&downloadID, &version, &status, &size, &code, &message)
	if errors.Is(err, sql.ErrNoRows) {
		return row{}, nil
	}
	if err != nil {
		return row{}, err
	}
	r.present = true
	r.DownloadID = downloadID.String
	r.Version = version.String
	r.Status = status.String
	r.Size = size
	if code.String != "" {
		r.Error = &download.Error{Code: code.String, Message: message.String}
	}
	return r, nil
}

func (s *Service) setRow(r row, id string) error {
	stamp := now()
	code, message := "", ""
	if r.Error != nil {
		code, message = r.Error.Code, r.Error.Message
	}
	var size any
	if r.Size.Valid {
		size = r.Size.Int64
	}
	_, err := s.db.Exec(`INSERT INTO translation_models(id,download_id,version,status,size_bytes,error_code,error_message,updated_at) VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET download_id=excluded.download_id,version=excluded.version,status=excluded.status,size_bytes=excluded.size_bytes,error_code=excluded.error_code,error_message=excluded.error_message,updated_at=excluded.updated_at`,
		id, nullable(r.DownloadID), r.Version, r.Status, size, code, message, stamp)
	return err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Service) ownedDirs() bool {
	current, err := os.Lstat(s.dataRoot)
	if err != nil || !current.IsDir() || current.Mode()&os.ModeSymlink != 0 {
		return false
	}
	original, err := s.root.Stat(".")
	if err != nil || !os.SameFile(current, original) {
		return false
	}
	for name, handle := range map[string]*os.Root{"content/translation": s.models, "downloads": s.downloads} {
		cur, err := s.root.Lstat(name)
		if err != nil || !cur.IsDir() || cur.Mode()&os.ModeSymlink != 0 {
			return false
		}
		orig, err := handle.Stat(".")
		if err != nil || !os.SameFile(cur, orig) {
			return false
		}
	}
	return true
}

// recover reconciles interrupted work without networking. Staging leftovers
// are removed (unknown user files are never touched). A published directory
// is trusted only with a valid install receipt bound to the current pin plus
// verified payload hashes; loose metadata directories are never adopted.
// Vanished models surface as recoverable errors.
func (s *Service) recover() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".staging-") {
			_ = os.RemoveAll(filepath.Join(s.dir, e.Name()))
		}
	}
	for _, def := range Definitions(s.pins) {
		r, err := s.getRow(def.ID)
		if err != nil {
			return err
		}
		valid := validateReceipt(filepath.Join(s.dir, def.ID), def, def.Acquisition) == nil
		switch {
		case valid && r.Status == StatusReady:
			// Steady state.
		case valid:
			// Crash between atomic publish and durable commit: the receipt
			// proves the payload, so adopt idempotently and mark a claimed
			// artifact consumed.
			pin := def.Acquisition
			if r.DownloadID != "" {
				if err := s.commitReady(r.DownloadID, def.ID, pin.Version, pin.ExpectedSize); err != nil {
					return err
				}
			} else if err := s.setRow(row{Version: pin.Version, Status: StatusReady, Size: sql.NullInt64{Int64: pin.ExpectedSize, Valid: true}}, def.ID); err != nil {
				return err
			}
			s.logf("recovered model id=%s code=ok", def.ID)
		case !valid && r.Status == StatusReady:
			if err := s.setRow(row{DownloadID: r.DownloadID, Version: r.Version, Status: StatusError, Size: r.Size, Error: ErrFiles}, def.ID); err != nil {
				return err
			}
			s.logf("recovered model id=%s code=artifact_unavailable", def.ID)
		case !valid && r.Status == StatusInstalling:
			if err := s.setRow(row{DownloadID: r.DownloadID, Version: r.Version, Status: StatusError, Size: r.Size, Error: &download.Error{Code: "interrupted", Message: "İşlem uygulama kapanınca kesildi. Tekrar deneyin."}}, def.ID); err != nil {
				return err
			}
			s.logf("recovered model id=%s code=interrupted", def.ID)
		default:
			// Error or missing rows with invalid payloads stay as-is; the
			// explicit install path repairs them without deleting anything.
		}
	}
	return nil
}

// commitReady marks the artifact consumed and the model ready atomically.
// Persistence failures surface as storage errors instead of silent skew.
func (s *Service) commitReady(downloadID, id, version string, size int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return download.ErrStorage
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE downloads SET artifact_state='consumed' WHERE id=? AND artifact_owner=? AND status='completed' AND artifact_state IN ('claimed','consumed')`, downloadID, downloadID)
	if err != nil {
		return download.ErrStorage
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return download.ErrStorage
	}
	res, err = tx.Exec(`UPDATE translation_models SET download_id=?,version=?,status='ready',size_bytes=?,error_code='',error_message='',updated_at=? WHERE id=?`, downloadID, version, size, now(), id)
	if err != nil {
		return download.ErrStorage
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return download.ErrStorage
	}
	if err := tx.Commit(); err != nil {
		return download.ErrStorage
	}
	return nil
}

// RuntimeAvailable reports whether the frozen worker binary is present.
func (s *Service) RuntimeAvailable() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && binaryAvailable(s.binary)
}

// snapshot builds the public model view. Manager access is best-effort: a
// vanished download row surfaces as null, never as a failure. Callers holding
// s.mu pass installing explicitly; snapshot resolves it otherwise.
func (s *Service) snapshot(def Definition, m *download.Manager) (Model, error) {
	s.mu.Lock()
	installing := s.active[def.ID]
	s.mu.Unlock()
	return s.snapshotWith(def, m, installing)
}

func (s *Service) snapshotWith(def Definition, m *download.Manager, installing bool) (Model, error) {
	r, err := s.getRow(def.ID)
	if err != nil {
		return Model{}, download.ErrStorage
	}
	out := Model{
		ID: def.ID, SourceLanguage: def.SourceLang, TargetLanguage: def.TargetLang,
		Status: StatusMissing,
	}
	if r.Version != "" {
		out.Version = r.Version
	} else if def.Acquisition.Version != "" {
		out.Version = def.Acquisition.Version
	}
	if r.Size.Valid {
		out.SizeBytes = r.Size.Int64
	} else if def.Acquisition.ExpectedSize > 0 {
		out.SizeBytes = def.Acquisition.ExpectedSize
	}
	switch {
	case installing || r.Status == StatusInstalling:
		out.Status = StatusInstalling
	case r.Status == StatusReady && s.quickValid(def):
		out.Status = StatusReady
	case r.Status == StatusError:
		out.Status = StatusError
		out.Error = r.Error
		if out.Error == nil {
			out.Error = ErrFiles
		}
	case r.Status == StatusReady:
		out.Status = StatusError
		out.Error = ErrFiles
	}
	out.Download = s.findDownload(def, r.DownloadID, m)
	return out, nil
}

// findDownload resolves the model's download view. The durable latest
// association wins; otherwise the newest live manager row for this model is
// shown so the UI can install automatically once the user's own download
// completes. The manager remains the source of truth; failures surface as null.
func (s *Service) findDownload(def Definition, associated string, m *download.Manager) *download.Snapshot {
	if m == nil {
		return nil
	}
	if associated != "" {
		if snap, err := m.Get(associated); err == nil {
			dup := snap
			return &dup
		}
	}
	items, err := m.List()
	if err != nil {
		return nil
	}
	for _, item := range items {
		if item.Provider == Provider && item.ProviderPackageID == def.ID && item.Status != "cancelled" {
			dup := item
			return &dup
		}
	}
	return nil
}

// List is read-only: it never writes the registry and never starts downloads.
// One model's stored error never disables the other direction; only a broken
// registry fails the whole listing.
func (s *Service) List(m *download.Manager) (Status, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return Status{}, download.ErrStorage
	}
	defs := Definitions(s.pins)
	models := make([]Model, 0, len(defs))
	for _, def := range defs {
		model, err := s.snapshot(def, m)
		if err != nil {
			return Status{}, err
		}
		models = append(models, model)
	}
	return Status{Models: models, RuntimeAvailable: s.RuntimeAvailable(), MaxCharacters: MaxCharacters}, nil
}

func titleOf(def Definition) string {
	if def.Acquisition.Title != "" {
		return def.Acquisition.Title
	}
	return def.Title
}

func validChecksum(pin Acquisition) bool {
	b, err := hex.DecodeString(strings.ToLower(strings.TrimSpace(pin.ChecksumValue)))
	return pin.ChecksumValue != "" && err == nil && len(b) == sha256.Size
}

// StartDownload creates a generic download for an explicit user action.
// Independent models: exactly one direction is queued, nothing else. A model
// that is already ready at the same pinned version is not re-downloaded. The
// latest explicit download association is stored durably so GET never shows a
// stale download after a failed install is superseded.
func (s *Service) StartDownload(m *download.Manager, modelID string) (download.Snapshot, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed || m == nil {
		return download.Snapshot{}, download.ErrStorage
	}
	def, ok := Find(s.pins, modelID)
	if !ok {
		return download.Snapshot{}, ErrNotFound
	}
	pin := def.Acquisition
	if !def.Verified() || !validChecksum(pin) {
		return download.Snapshot{}, ErrMetadata
	}
	r, err := s.getRow(def.ID)
	if err != nil {
		return download.Snapshot{}, download.ErrStorage
	}
	if r.Status == StatusReady && r.Version == pin.Version && s.quickValid(def) {
		return download.Snapshot{}, ErrCollision
	}
	total, expected := pin.TotalBytes, pin.ExpectedSize
	snap, err := m.Create(download.Descriptor{
		Provider: Provider, ProviderPackageID: def.ID,
		Title: titleOf(def), Variant: def.SourceLang + "-" + def.TargetLang,
		FileName: pin.FileName, SourceURL: pin.SourceURL, TotalBytes: &total,
		ContentType: ContentType, Language: def.SourceLang + "-" + def.TargetLang,
		Version: pin.Version, ExpectedSize: &expected,
		ChecksumAlgorithm: "sha256", ChecksumValue: strings.ToLower(pin.ChecksumValue),
	})
	if err != nil {
		return download.Snapshot{}, err
	}
	if r.present {
		r.DownloadID = snap.ID
		if err := s.setRow(r, def.ID); err != nil {
			return download.Snapshot{}, download.ErrStorage
		}
	}
	s.logf("download model=%s code=ok", def.ID)
	return snap, nil
}

// Install claims a completed download and installs it asynchronously.
// It returns installed=true only when the model is already ready. A corrupt
// or foreign final directory is staged aside (never deleted) so an explicit
// reinstall repairs the model.
func (s *Service) Install(m *download.Manager, modelID, downloadID string) (Model, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || m == nil {
		return Model{}, false, download.ErrStorage
	}
	def, ok := Find(s.pins, modelID)
	if !ok {
		return Model{}, false, ErrNotFound
	}
	if strings.TrimSpace(downloadID) == "" {
		return Model{}, false, ErrInvalid
	}
	if !s.ownedDirs() {
		return Model{}, false, ErrUnsafe
	}
	r, err := s.getRow(def.ID)
	if err != nil {
		return Model{}, false, download.ErrStorage
	}
	final := filepath.Join(s.dir, def.ID)
	_, statErr := s.models.Lstat(def.ID)
	finalExists := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return Model{}, false, ErrUnsafe
	}
	quick := finalExists && s.quickValid(def)
	if r.Status == StatusReady && quick {
		if r.DownloadID == downloadID {
			model, err := s.snapshotWith(def, m, false)
			return model, true, err
		}
		return Model{}, false, ErrCollision
	}
	if s.active[def.ID] {
		model, err := s.snapshotWith(def, m, true)
		return model, false, err
	}
	artifact, err := m.ClaimCompleted(downloadID, downloadID)
	if err != nil {
		return Model{}, false, err
	}
	if err := s.checkArtifact(def, artifact); err != nil {
		return Model{}, false, err
	}
	// Open the artifact once through the confined downloads root: the
	// descriptor below is hashed, extracted and re-validated without reopening
	// by path, closing the verify/use TOCTOU window.
	if artifact.TempPath != filepath.Join(s.downloadsDir, artifact.ID+".part") {
		return Model{}, false, ErrUnsafe
	}
	fh, err := s.downloads.OpenFile(artifact.ID+".part", os.O_RDONLY, 0)
	if err != nil {
		return Model{}, false, ErrUnsafe
	}
	fi, err := fh.Stat()
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != def.Acquisition.ExpectedSize {
		fh.Close()
		s.setRow(row{DownloadID: downloadID, Version: def.Acquisition.Version, Status: StatusError, Error: ErrVerify}, def.ID)
		return Model{}, false, ErrVerify
	}
	if finalExists && quick && r.Status != StatusError {
		// A verified payload is present but not installed under this row
		// (orphan or concurrent publish): refuse to replace it. Restart
		// recovery adopts orphan verified directories.
		fh.Close()
		return Model{}, false, ErrCollision
	}
	// Repair: a corrupt, foreign or previously-failed final directory is
	// staged aside within the same parent. Nothing is ever deleted, and no
	// directory outside our model path is touched.
	if finalExists {
		if err := os.Rename(final, filepath.Join(s.dir, brokenName(def.ID))); err != nil {
			fh.Close()
			return Model{}, false, ErrCollision
		}
		s.logf("install model=%s code=repaired", def.ID)
	}
	if err := s.setRow(row{DownloadID: downloadID, Version: def.Acquisition.Version, Status: StatusInstalling}, def.ID); err != nil {
		fh.Close()
		return Model{}, false, download.ErrStorage
	}
	s.active[def.ID] = true
	s.wg.Add(1)
	go s.work(artifact, fh, def)
	s.logf("install model=%s code=accepted", def.ID)
	model, err := s.snapshotWith(def, m, true)
	return model, false, err
}

// checkArtifact validates trusted manager metadata against verified pins.
// No hashing or extraction happens here.
func (s *Service) checkArtifact(def Definition, a download.Artifact) error {
	pin := def.Acquisition
	if !def.Verified() || !validChecksum(pin) {
		return ErrMetadata
	}
	if a.Provider != Provider || a.ProviderPackageID != def.ID {
		return ErrMetadata
	}
	if strings.ToLower(a.ChecksumAlgorithm) != "sha256" || strings.ToLower(strings.TrimSpace(a.ChecksumValue)) != strings.ToLower(pin.ChecksumValue) {
		return ErrChecksum
	}
	if a.ExpectedSize == nil || *a.ExpectedSize != pin.ExpectedSize || pin.ExpectedSize <= 0 {
		return ErrMetadata
	}
	if a.Version != pin.Version {
		return ErrMetadata
	}
	return nil
}

func (s *Service) setError(id string, problem *download.Error) {
	r, err := s.getRow(id)
	if err != nil {
		s.logf("install model=%s code=storage_error", id)
		return
	}
	r.Status = StatusError
	r.Error = problem
	_ = s.setRow(r, id)
	s.logf("install model=%s code=%s", id, problem.Code)
}

// work verifies, extracts, publishes and commits one model. The artifact file
// stays open from hashing through the pre-publish ownership recheck, so the
// bytes verified are the bytes extracted.
func (s *Service) work(a download.Artifact, f *os.File, def Definition) {
	pin := def.Acquisition
	size := pin.ExpectedSize
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		delete(s.active, def.ID)
		s.mu.Unlock()
	}()
	defer f.Close()
	fail := func(problem *download.Error) { s.setError(def.ID, problem) }
	if err := s.ctx.Err(); err != nil {
		fail(ErrFiles)
		return
	}
	if !s.ownedDirs() {
		fail(ErrUnsafe)
		return
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		fail(ErrVerify)
		return
	}
	h := sha256.New()
	buf := make([]byte, 256*1024)
	for {
		if err := s.ctx.Err(); err != nil {
			fail(ErrFiles)
			return
		}
		n, readErr := f.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			fail(ErrVerify)
			return
		}
	}
	if hex.EncodeToString(h.Sum(nil)) != strings.ToLower(pin.ChecksumValue) {
		fail(ErrVerify)
		return
	}
	if err := s.ctx.Err(); err != nil {
		fail(ErrFiles)
		return
	}
	zr, err := zip.NewReader(f, size)
	if err != nil {
		fail(ErrVerify)
		return
	}
	members, err := planMembers(zr)
	if err != nil {
		fail(ErrVerify)
		return
	}
	staging := filepath.Join(s.dir, stageName(def.ID))
	if err := os.Mkdir(staging, 0o700); err != nil {
		fail(download.ErrStorage)
		return
	}
	done := false
	defer func() {
		if !done {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := s.checkpoint(); err != nil {
		fail(ErrFiles)
		return
	}
	if err := extractStaging(s.ctx, staging, members); err != nil {
		if s.ctx.Err() != nil {
			fail(ErrFiles)
			return
		}
		var problem *download.Error
		if errors.As(err, &problem) {
			fail(problem)
		} else {
			fail(ErrVerify)
		}
		return
	}
	if err := checkStagedModel(staging, def.SourceLang, def.TargetLang, pin.Version); err != nil {
		fail(ErrVerify)
		return
	}
	if err := writeReceipt(staging, def, pin); err != nil {
		var problem *download.Error
		if errors.As(err, &problem) {
			fail(problem)
		} else {
			fail(ErrVerify)
		}
		return
	}
	if err := s.checkpoint(); err != nil {
		fail(ErrFiles)
		return
	}
	// Pre-publish: the artifact must still be the owned regular file with the
	// exact pinned size, and the directories must still be owned. This closes
	// the window between verification and the atomic rename.
	if !s.ownedDirs() {
		fail(ErrUnsafe)
		return
	}
	current, err := f.Stat()
	if err != nil || !current.Mode().IsRegular() || current.Size() != size {
		fail(ErrUnsafe)
		return
	}
	published, err := s.downloads.Lstat(a.ID + ".part")
	if err != nil || !published.Mode().IsRegular() || !os.SameFile(current, published) || published.Size() != size {
		fail(ErrUnsafe)
		return
	}
	final := filepath.Join(s.dir, def.ID)
	if _, err := os.Lstat(final); err == nil {
		fail(ErrCollision)
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		fail(ErrUnsafe)
		return
	}
	if err := os.Rename(staging, final); err != nil {
		if errors.Is(err, os.ErrExist) {
			fail(ErrCollision)
		} else {
			fail(download.ErrStorage)
		}
		return
	}
	done = true
	if err := validateReceipt(final, def, pin); err != nil {
		fail(ErrVerify)
		return
	}
	if err := s.commitReady(a.ID, def.ID, pin.Version, size); err != nil {
		fail(download.ErrStorage)
		return
	}
	s.logf("install model=%s code=ready", def.ID)
}

func (s *Service) checkpoint() error {
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	default:
		return nil
	}
}

// Translate converts text between exactly tr and en using the persistent
// worker. Input is validated; source text is never logged or persisted.
// Queue waits and in-flight work are bounded by both the caller and the
// service lifetime; the warm worker process is reused across requests.
func (s *Service) Translate(ctx context.Context, source, target, text string) (string, error) {
	def, ok := Pair(s.pins, source, target)
	if !ok {
		return "", ErrInvalid
	}
	if !utf8.ValidString(text) {
		return "", ErrInvalid
	}
	if strings.TrimSpace(text) == "" {
		return "", ErrInvalid
	}
	if len([]rune(text)) > MaxCharacters {
		return "", ErrInvalid
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return "", download.ErrStorage
	}
	r, err := s.getRow(def.ID)
	if err != nil {
		return "", download.ErrStorage
	}
	if r.Status != StatusReady || !s.quickValid(def) {
		return "", ErrState
	}
	merged, cancel := mergeDone(ctx, s.ctx)
	defer cancel()
	release, err := translateGate.acquire(merged)
	if err != nil {
		if s.ctx.Err() != nil {
			return "", download.ErrStorage
		}
		return "", err
	}
	defer release()
	s.procMu.Lock()
	// Closed is owned by s.mu; read it under that mutex (never procMu).
	s.mu.Lock()
	procClosed := s.closed
	s.mu.Unlock()
	if procClosed {
		s.procMu.Unlock()
		return "", download.ErrStorage
	}
	proc := s.proc
	if proc == nil {
		var startErr error
		proc, startErr = startWorker(s.binary, s.dir)
		if startErr != nil {
			s.procMu.Unlock()
			var problem *download.Error
			if errors.As(startErr, &problem) {
				return "", problem
			}
			return "", ErrEngine
		}
		s.proc = proc
	}
	s.procMu.Unlock()
	payload, err := json.Marshal(workerRequest{Cmd: "translate", SourceLanguage: source, TargetLanguage: target, Text: text})
	if err != nil {
		s.dropWorker(proc)
		return "", ErrEngine
	}
	translated, err := roundTrip(merged, proc, payload, s.timeout)
	if err != nil {
		s.dropWorker(proc)
		if s.ctx.Err() != nil {
			return "", download.ErrStorage
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		var problem *download.Error
		if errors.As(err, &problem) {
			return "", problem
		}
		return "", ErrEngine
	}
	if !utf8.ValidString(translated) || strings.TrimSpace(translated) == "" || len([]rune(translated)) > maxTranslatedRunes {
		s.dropWorker(proc)
		return "", ErrEngine
	}
	return translated, nil
}

func (s *Service) dropWorker(p *workerProc) {
	s.procMu.Lock()
	defer s.procMu.Unlock()
	if s.proc == p {
		killWorker(p)
		s.proc = nil
	}
}

// Close interrupts in-flight translation promptly, joins install workers and
// releases the owned handles.
func (s *Service) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	s.procMu.Lock()
	killWorker(s.proc)
	s.proc = nil
	s.procMu.Unlock()
	// Join install workers; then drain the global gate so an in-flight
	// request killed above has returned before Close reports done.
	s.wg.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if release, err := translateGate.acquire(ctx); err == nil {
		release()
	}
	var err error
	for _, handle := range []*os.Root{s.models, s.downloads, s.root} {
		if handle != nil {
			err = errors.Join(err, handle.Close())
		}
	}
	return err
}
