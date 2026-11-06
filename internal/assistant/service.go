package assistant

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"engelsiz/internal/download"
)

// Options configures the assistant service.
//
// Pins carries verified per-model acquisition metadata. A nil map selects the
// DefaultPins; an explicitly empty map disables all models. Pins are the only
// source of download URLs, sizes and checksums. RuntimePin overrides the
// platform runtime pin; nil selects the platform default. Binary may hold an
// explicit absolute custom runtime path; the ENGELSIZ_LLAMA_BINARY
// environment variable is honored the same way. No PATH lookup or cloud
// fallback is ever performed. Missing runtime never prevents New.
type Options struct {
	Pins       map[string]Acquisition
	RuntimePin *RuntimeAcquisition
	Binary     string
	Logger     *log.Logger
}

// Service owns the durable assistant registry, model/runtime installation and
// the inference busy lease. Missing runtime binary never fails New; UseModel
// then reports engine_unavailable while the rest of the app keeps running.
type Service struct {
	db         *sql.DB
	dataRoot   string
	dir        string
	modelsDir  string
	runtimeDir string
	dlDir      string

	root    *os.Root
	models  *os.Root
	rt      *os.Root
	dls     *os.Root
	runtime *os.Root // alias of rt kept for receipt.go naming

	pins    map[string]Acquisition
	rtPin   *RuntimeAcquisition
	binary  string
	hasCustom bool
	customBinary string
	logger  *log.Logger
	platKey string

	mu     sync.Mutex
	lease  bool // inference in-flight via UseModel
	active map[string]bool
	closed bool

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Model is the pinned inference snapshot handed to the engine callback.
type Model struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	Binary      string `json:"binary"`
	ContextSize int    `json:"context_size"`
	MaxTokens   int    `json:"max_tokens"`
}

// Profile is the public per-profile view.
type Profile struct {
	ID           string             `json:"id"`
	Title        string             `json:"title"`
	ModelName    string             `json:"model_name"`
	Quantization string             `json:"quantization"`
	SizeBytes    int64              `json:"size_bytes"`
	RAMGB        int                `json:"ram_gb"`
	ContextSize  int                `json:"context_size"`
	License      string             `json:"license"`
	Source       string             `json:"source"`
	Status       string             `json:"status"`
	Download     *download.Snapshot `json:"download"`
	Error        *download.Error    `json:"error"`
}

// RuntimeStatus is the public runtime view.
type RuntimeStatus struct {
	Available bool               `json:"available"`
	Status    string             `json:"status"`
	Download  *download.Snapshot `json:"download"`
	Error     *download.Error    `json:"error"`
	Version   string             `json:"version"`
}

// Status is the GET /api/assistant response body.
type Status struct {
	Profiles             []Profile     `json:"profiles"`
	ActiveProfileID      string        `json:"active_profile_id"`
	RecommendedProfileID string        `json:"recommended_profile_id"`
	Hardware             Hardware      `json:"hardware"`
	Runtime              RuntimeStatus `json:"runtime"`
	Busy                 bool          `json:"busy"`
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
		s.logger.Printf("assistant "+format, args...)
	}
}

// mergeDone returns a context cancelled when either the caller or the service
// shuts down, bounding queued and in-flight inference by service lifetime.
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
	for _, name := range []string{"content", "content/assistant", "content/assistant/models", "content/assistant/runtime", "downloads"} {
		if err := root.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if fi, err := root.Lstat(name); err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return nil, ErrUnsafe
		}
	}
	models, err := root.OpenRoot("content/assistant/models")
	if err != nil {
		return nil, err
	}
	defer func() {
		if !ok {
			models.Close()
		}
	}()
	rt, err := root.OpenRoot("content/assistant/runtime")
	if err != nil {
		models.Close()
		return nil, err
	}
	defer func() {
		if !ok {
			rt.Close()
		}
	}()
	dls, err := root.OpenRoot("downloads")
	if err != nil {
		models.Close()
		rt.Close()
		return nil, err
	}
	defer func() {
		if !ok {
			dls.Close()
		}
	}()
	if opts.Pins == nil {
		opts.Pins = DefaultPins
	}
	pins := make(map[string]Acquisition, len(opts.Pins))
	for id, pin := range opts.Pins {
		pins[id] = pin
	}
	var rtPin *RuntimeAcquisition
	if opts.RuntimePin != nil {
		dup := *opts.RuntimePin
		rtPin = &dup
	} else if pin, found := DefaultRuntimePins[runtime.GOOS+"/"+runtime.GOARCH]; found {
		dup := pin
		rtPin = &dup
	}
	ctx, cancel := context.WithCancel(context.Background())
	resolvedBinary := resolveRuntimeBinary(opts.Binary, filepath.Join(absolute, "content", "assistant", "runtime"))
	hasCustom := false
	customBinary := ""
	if opts.Binary != "" {
		hasCustom = true
		customBinary = opts.Binary
	} else if dev := os.Getenv("ENGELSIZ_LLAMA_BINARY"); dev != "" {
		hasCustom = true
		if filepath.IsAbs(dev) {
			customBinary = dev
		} else {
			customBinary = ""
		}
	}
	// When no explicit custom binary, resolvedBinary is the legacy default
	// path; effectiveBinary ignores it unless a validated receipt binds it.
	// hasCustom preserves the explicit override verbatim.
	if hasCustom {
		resolvedBinary = customBinary
	}
	s := &Service{
		db: db, dataRoot: absolute,
		dir:        filepath.Join(absolute, "content", "assistant"),
		modelsDir:  filepath.Join(absolute, "content", "assistant", "models"),
		runtimeDir: filepath.Join(absolute, "content", "assistant", "runtime"),
		dlDir:      filepath.Join(absolute, "downloads"),
		root:       root, models: models, rt: rt, runtime: rt, dls: dls,
		pins: pins, rtPin: rtPin, binary: resolvedBinary,
		hasCustom: hasCustom, customBinary: customBinary,
		logger: opts.Logger, platKey: runtime.GOOS + "/" + runtime.GOARCH,
		active: map[string]bool{}, ctx: ctx, cancel: cancel,
	}
	if err := s.recover(); err != nil {
		cancel()
		return nil, err
	}
	ok = true
	return s, nil
}

func (s *Service) getRow(table, id string) (row, error) {
	var r row
	var size sql.NullInt64
	var code, message, downloadID, version, status sql.NullString
	q := `SELECT download_id,version,status,size_bytes,error_code,error_message FROM ` + table + ` WHERE id=?`
	err := s.db.QueryRow(q, id).Scan(&downloadID, &version, &status, &size, &code, &message)
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

func (s *Service) setRow(table string, r row, id string) error {
	stamp := now()
	code, message := "", ""
	if r.Error != nil {
		code, message = r.Error.Code, r.Error.Message
	}
	var size any
	if r.Size.Valid {
		size = r.Size.Int64
	}
	q := `INSERT INTO ` + table + `(id,download_id,version,status,size_bytes,error_code,error_message,updated_at) VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET download_id=excluded.download_id,version=excluded.version,status=excluded.status,size_bytes=excluded.size_bytes,error_code=excluded.error_code,error_message=excluded.error_message,updated_at=excluded.updated_at`
	_, err := s.db.Exec(q, id, nullable(r.DownloadID), r.Version, r.Status, size, code, message, stamp)
	return err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Service) getState(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM assistant_state WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *Service) setState(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO assistant_state(key,value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
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
	for name, handle := range map[string]*os.Root{
		"content/assistant/models":  s.models,
		"content/assistant/runtime": s.rt,
		"downloads":                 s.dls,
	} {
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
// is trusted only with a receipt bound to the current pin; payloads are never
// re-hashed here. Install hashes payload bytes twice: once streaming the
// downloaded artifact, once streaming the staged copy into place; the
// receipt binds the staged tree to that digest. Boot and List only check
// receipt binding plus presence/size, never payload bytes. Vanished models
// surface as recoverable errors.
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
	modelEntries, err := os.ReadDir(s.modelsDir)
	if err != nil {
		return err
	}
	for _, e := range modelEntries {
		if strings.HasPrefix(e.Name(), ".staging-") {
			_ = os.RemoveAll(filepath.Join(s.modelsDir, e.Name()))
		}
	}
	for _, def := range Definitions(s.pins) {
		r, err := s.getRow("assistant_models", def.ID)
		if err != nil {
			return err
		}
		valid := s.quickValidModel(def)
		switch {
		case valid && r.Status == StatusReady:
			// Steady state.
		case valid:
			pin := def.Acquisition
			if r.DownloadID != "" {
				if err := s.commitReady(r.DownloadID, def.ID, pin.Version, pin.ExpectedSize); err != nil {
					return err
				}
			} else if err := s.setRow("assistant_models", row{Version: pin.Version, Status: StatusReady, Size: sql.NullInt64{Int64: pin.ExpectedSize, Valid: true}}, def.ID); err != nil {
				return err
			}
			s.logf("recovered model id=%s code=ok", def.ID)
		case !valid && r.Status == StatusReady:
			if err := s.setRow("assistant_models", row{DownloadID: r.DownloadID, Version: r.Version, Status: StatusError, Size: r.Size, Error: ErrFiles}, def.ID); err != nil {
				return err
			}
			s.logf("recovered model id=%s code=artifact_unavailable", def.ID)
		case !valid && r.Status == StatusInstalling:
			if err := s.setRow("assistant_models", row{DownloadID: r.DownloadID, Version: r.Version, Status: StatusInstalling, Size: r.Size, Error: nil}, def.ID); err != nil {
				return err
			}
			r2, err := s.getRow("assistant_models", def.ID)
			if err != nil {
				return err
			}
			r2.Status = StatusError
			r2.Error = &download.Error{Code: "interrupted", Message: "İşlem uygulama kapanınca kesildi. Tekrar deneyin."}
			if err := s.setRow("assistant_models", r2, def.ID); err != nil {
				return err
			}
			s.logf("recovered model id=%s code=interrupted", def.ID)
		default:
			// Error or missing rows with invalid payloads stay as-is; the
			// explicit install path repairs them without deleting anything.
		}
	}
	if s.rtPin != nil && s.rtPin.Verified() {
		r, err := s.getRow("assistant_runtime", RuntimePackageID)
		if err != nil {
			return err
		}
		valid := s.quickValidRuntime(*s.rtPin)
		switch {
		case valid && r.Status == StatusReady:
		case valid:
			if r.DownloadID != "" {
				if err := s.commitRuntimeReady(r.DownloadID, s.rtPin.Version, s.rtPin.ExpectedSize); err != nil {
					return err
				}
			} else if err := s.setRow("assistant_runtime", row{Version: s.rtPin.Version, Status: StatusReady, Size: sql.NullInt64{Int64: s.rtPin.ExpectedSize, Valid: true}}, RuntimePackageID); err != nil {
				return err
			}
			s.logf("recovered runtime code=ok")
		case !valid && r.Status == StatusReady:
			if err := s.setRow("assistant_runtime", row{DownloadID: r.DownloadID, Version: r.Version, Status: StatusError, Size: r.Size, Error: ErrFiles}, RuntimePackageID); err != nil {
				return err
			}
			s.logf("recovered runtime code=artifact_unavailable")
		case !valid && r.Status == StatusInstalling:
			r.Status = StatusError
			r.Error = &download.Error{Code: "interrupted", Message: "İşlem uygulama kapanınca kesildi. Tekrar deneyin."}
			if err := s.setRow("assistant_runtime", r, RuntimePackageID); err != nil {
				return err
			}
			s.logf("recovered runtime code=interrupted")
		default:
		}
	}
	return nil
}

// commitReady marks the artifact consumed and the model ready atomically.
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
	res, err = tx.Exec(`UPDATE assistant_models SET download_id=?,version=?,status='ready',size_bytes=?,error_code='',error_message='',updated_at=? WHERE id=?`, downloadID, version, size, now(), id)
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

func (s *Service) commitRuntimeReady(downloadID, version string, size int64) error {
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
	res, err = tx.Exec(`UPDATE assistant_runtime SET download_id=?,version=?,status='ready',size_bytes=?,error_code='',error_message='',updated_at=? WHERE id='runtime'`, downloadID, version, size, now())
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

// busyLocked reports whether inference or any install is in-flight. Callers
// must hold s.mu.
func (s *Service) busyLocked() bool {
	return s.lease || len(s.active) > 0
}

func (s *Service) snapshotProfile(def Definition, m *download.Manager, installing bool) (Profile, error) {
	r, err := s.getRow("assistant_models", def.ID)
	if err != nil {
		return Profile{}, download.ErrStorage
	}
	pin := def.Acquisition
	out := Profile{
		ID: def.ID, Title: def.Title, ModelName: pin.ModelName, Quantization: pin.Quantization,
		SizeBytes: pin.ExpectedSize, RAMGB: pin.RAMGB, ContextSize: pin.ContextSize,
		License: pin.License, Source: pin.Source, Status: StatusMissing,
	}
	if r.Size.Valid {
		out.SizeBytes = r.Size.Int64
	}
	switch {
	case installing || r.Status == StatusInstalling:
		out.Status = StatusInstalling
	case r.Status == StatusReady && s.quickValidModel(def):
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
	default:
		// A completed but uninstalled user download surfaces as downloaded so
		// the UI can install without re-downloading.
		if snap := s.findDownload(Provider, def.ID, r.DownloadID, m); snap != nil && snap.Status == "completed" {
			out.Status = StatusDownloaded
			out.Download = snap
			break
		}
		if snap := s.findDownload(Provider, def.ID, r.DownloadID, m); snap != nil && (snap.Status == "downloading" || snap.Status == "queued" || snap.Status == "paused") {
			out.Status = StatusDownloading
			out.Download = snap
			break
		}
		out.Status = StatusMissing
	}
	if out.Download == nil {
		out.Download = s.findDownload(Provider, def.ID, r.DownloadID, m)
	}
	return out, nil
}

// findDownload resolves the download view. The durable latest association
// wins; otherwise the newest live manager row for this package is shown. The
// manager remains the source of truth; failures surface as null. No downloads
// are ever created here.
func (s *Service) findDownload(provider, packageID, associated string, m *download.Manager) *download.Snapshot {
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
		if item.Provider == provider && item.ProviderPackageID == packageID && item.Status != "cancelled" {
			dup := item
			return &dup
		}
	}
	return nil
}

// effectiveBinary resolves the trusted runtime binary. An explicit absolute
// custom override (Options.Binary or ENGELSIZ_LLAMA_BINARY) wins verbatim so
// custom binaries keep working even on unsupported platforms. Otherwise the
// binary is derived from the validated installed receipt bound to the current
// pin plus the confined tree (quickValidRuntime binding): nested official
// layouts such as bin/llama-server resolve to their actual path. No
// unreceipted local runtime path is ever trusted; without a valid receipt the
// result is empty (unavailable).
func (s *Service) effectiveBinary() string {
	if s.hasCustom {
		return s.customBinary
	}
	if s.rtPin == nil || !s.rtPin.Verified() {
		return ""
	}
	pin := *s.rtPin
	if !s.quickValidRuntime(pin) {
		return ""
	}
	r, err := readReceipt(s.runtimeDir)
	if err != nil {
		return ""
	}
	if checkRuntimeBinding(r, pin) != nil || r.Binary == "" {
		return ""
	}
	return filepath.Join(s.runtimeDir, filepath.FromSlash(r.Binary))
}

func (s *Service) snapshotRuntime(m *download.Manager, installing bool) (RuntimeStatus, error) {
	out := RuntimeStatus{Status: StatusMissing}
	if s.rtPin == nil || !s.rtPin.Verified() {
		out.Status = StatusUnavailable
		out.Error = &download.Error{Code: "invalid_state", Message: "Bu platform için doğrulanmış asistan motoru paketi yok."}
		return out, nil
	}
	pin := *s.rtPin
	out.Version = pin.Version
	r, err := s.getRow("assistant_runtime", RuntimePackageID)
	if err != nil {
		return RuntimeStatus{}, download.ErrStorage
	}
	bin := s.effectiveBinary()
	out.Available = binaryAvailable(bin)
	switch {
	case installing || r.Status == StatusInstalling:
		out.Status = StatusInstalling
	case r.Status == StatusReady && s.quickValidRuntime(pin) && out.Available:
		out.Status = StatusReady
	case r.Status == StatusReady && s.quickValidRuntime(pin):
		out.Status = StatusDownloaded
		out.Error = ErrEngine
	case r.Status == StatusError:
		out.Status = StatusError
		out.Error = r.Error
		if out.Error == nil {
			out.Error = ErrFiles
		}
	case r.Status == StatusReady:
		out.Status = StatusError
		out.Error = ErrFiles
	default:
		if snap := s.findDownload(Provider, RuntimePackageID, r.DownloadID, m); snap != nil && snap.Status == "completed" {
			out.Status = StatusDownloaded
			out.Download = snap
			break
		}
		if snap := s.findDownload(Provider, RuntimePackageID, r.DownloadID, m); snap != nil && (snap.Status == "downloading" || snap.Status == "queued" || snap.Status == "paused") {
			out.Status = StatusDownloading
			out.Download = snap
			break
		}
		out.Status = StatusMissing
	}
	if out.Download == nil {
		out.Download = s.findDownload(Provider, RuntimePackageID, r.DownloadID, m)
	}
	return out, nil
}

// List is read-only: it never writes the registry and never starts downloads.
// One profile's stored error never disables the others; only a broken
// registry fails the whole listing.
func (s *Service) List(m *download.Manager) (Status, error) {
	s.mu.Lock()
	closed := s.closed
	busy := s.busyLocked()
	s.mu.Unlock()
	if closed {
		return Status{}, download.ErrStorage
	}
	hw := detectHardware()
	activeID, err := s.getState("active_profile_id")
	if err != nil {
		return Status{}, download.ErrStorage
	}
	defs := Definitions(s.pins)
	profiles := make([]Profile, 0, len(defs))
	for _, def := range defs {
		s.mu.Lock()
		installing := s.active[def.ID]
		s.mu.Unlock()
		p, err := s.snapshotProfile(def, m, installing)
		if err != nil {
			return Status{}, err
		}
		profiles = append(profiles, p)
	}
	s.mu.Lock()
	rtInstalling := s.active[RuntimePackageID]
	s.mu.Unlock()
	rs, err := s.snapshotRuntime(m, rtInstalling)
	if err != nil {
		return Status{}, err
	}
	return Status{
		Profiles: profiles, ActiveProfileID: activeID,
		RecommendedProfileID: recommendedProfile(hw, s.pins),
		Hardware:             hw, Runtime: rs, Busy: busy,
	}, nil
}

// StartDownload creates a generic download for an explicit user action.
// Exactly one profile is queued, nothing else. A profile that is already
// ready at the same pinned version is not re-downloaded. The latest explicit
// download association is stored durably so GET never shows a stale download
// after a failed install is superseded.
func (s *Service) StartDownload(m *download.Manager, id string) (download.Snapshot, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed || m == nil {
		return download.Snapshot{}, download.ErrStorage
	}
	def, ok := Find(s.pins, id)
	if !ok {
		return download.Snapshot{}, ErrNotFound
	}
	pin := def.Acquisition
	if !def.Verified() || !validChecksum(pin.ChecksumValue) {
		return download.Snapshot{}, ErrMetadata
	}
	s.mu.Lock()
	if s.lease || s.active[def.ID] {
		s.mu.Unlock()
		return download.Snapshot{}, ErrBusy
	}
	s.mu.Unlock()
	r, err := s.getRow("assistant_models", def.ID)
	if err != nil {
		return download.Snapshot{}, download.ErrStorage
	}
	if r.Status == StatusReady && r.Version == pin.Version && s.quickValidModel(def) {
		return download.Snapshot{}, ErrCollision
	}
	total := pin.TotalBytes
	expected := pin.ExpectedSize
	snap, err := m.Create(download.Descriptor{
		Provider: Provider, ProviderPackageID: def.ID,
		Title: def.Title, Variant: pin.Quantization,
		FileName: pin.FileName, SourceURL: pin.SourceURL, TotalBytes: &total,
		ContentType: ContentTypeModel, Language: "tr",
		Version: pin.Version, ExpectedSize: &expected,
		ChecksumAlgorithm: "sha256", ChecksumValue: strings.ToLower(pin.ChecksumValue),
	})
	if err != nil {
		return download.Snapshot{}, err
	}
	if r.present {
		r.DownloadID = snap.ID
		if err := s.setRow("assistant_models", r, def.ID); err != nil {
			return download.Snapshot{}, download.ErrStorage
		}
	}
	s.logf("download profile=%s code=ok", def.ID)
	return snap, nil
}

// Install claims a completed download and stages the GGUF asynchronously. It
// returns installed=true only when the profile is already ready. A corrupt
// or foreign final directory is staged aside (never deleted) so an explicit
// reinstall repairs the profile.
func (s *Service) Install(m *download.Manager, id, downloadID string) (Profile, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || m == nil {
		return Profile{}, false, download.ErrStorage
	}
	def, ok := Find(s.pins, id)
	if !ok {
		return Profile{}, false, ErrNotFound
	}
	if strings.TrimSpace(downloadID) == "" {
		return Profile{}, false, ErrInvalid
	}
	if s.lease || s.active[def.ID] {
		p, serr := s.snapshotProfile(def, m, true)
		if serr != nil {
			return Profile{}, false, ErrBusy
		}
		return p, false, ErrBusy
	}
	if !s.ownedDirs() {
		return Profile{}, false, ErrUnsafe
	}
	r, err := s.getRow("assistant_models", def.ID)
	if err != nil {
		return Profile{}, false, download.ErrStorage
	}
	_, statErr := s.models.Lstat(def.ID)
	finalExists := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return Profile{}, false, ErrUnsafe
	}
	quick := finalExists && s.quickValidModel(def)
	if r.Status == StatusReady && quick {
		if r.DownloadID == downloadID {
			p, err := s.snapshotProfile(def, m, false)
			return p, true, err
		}
		return Profile{}, false, ErrCollision
	}
	artifact, err := m.ClaimCompleted(downloadID, downloadID)
	if err != nil {
		return Profile{}, false, err
	}
	if err := s.checkModelArtifact(def, artifact); err != nil {
		return Profile{}, false, err
	}
	if artifact.TempPath != filepath.Join(s.dlDir, artifact.ID+".part") {
		return Profile{}, false, ErrUnsafe
	}
	fh, err := s.dls.OpenFile(artifact.ID+".part", os.O_RDONLY, 0)
	if err != nil {
		return Profile{}, false, ErrUnsafe
	}
	fi, err := fh.Stat()
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != def.Acquisition.ExpectedSize {
		fh.Close()
		s.setRow("assistant_models", row{DownloadID: downloadID, Version: def.Acquisition.Version, Status: StatusError, Error: ErrVerify}, def.ID)
		return Profile{}, false, ErrVerify
	}
	if finalExists && quick && r.Status != StatusError {
		fh.Close()
		return Profile{}, false, ErrCollision
	}
	if finalExists {
		if err := os.Rename(filepath.Join(s.modelsDir, def.ID), filepath.Join(s.modelsDir, brokenName(def.ID))); err != nil {
			fh.Close()
			return Profile{}, false, ErrCollision
		}
		s.logf("install profile=%s code=repaired", def.ID)
	}
	if err := s.setRow("assistant_models", row{DownloadID: downloadID, Version: def.Acquisition.Version, Status: StatusInstalling}, def.ID); err != nil {
		fh.Close()
		return Profile{}, false, download.ErrStorage
	}
	s.active[def.ID] = true
	s.wg.Add(1)
	go s.workModel(artifact, fh, def)
	s.logf("install profile=%s code=accepted", def.ID)
	p, err := s.snapshotProfile(def, m, true)
	return p, false, err
}

// checkModelArtifact validates trusted manager metadata against verified
// pins. No hashing or extraction happens here.
func (s *Service) checkModelArtifact(def Definition, a download.Artifact) error {
	pin := def.Acquisition
	if !def.Verified() || !validChecksum(pin.ChecksumValue) {
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

func (s *Service) setModelError(id string, problem *download.Error) {
	r, err := s.getRow("assistant_models", id)
	if err != nil {
		s.logf("install profile=%s code=storage_error", id)
		return
	}
	r.Status = StatusError
	r.Error = problem
	_ = s.setRow("assistant_models", r, id)
	s.logf("install profile=%s code=%s", id, problem.Code)
}

// workModel streams the artifact SHA-256 once, stages the verified payload
// and publishes it atomically under a receipt bound to the pin.
func (s *Service) workModel(a download.Artifact, f *os.File, def Definition) {
	pin := def.Acquisition
	size := pin.ExpectedSize
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		delete(s.active, def.ID)
		s.mu.Unlock()
	}()
	defer f.Close()
	fail := func(problem *download.Error) { s.setModelError(def.ID, problem) }
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
	staging := filepath.Join(s.modelsDir, stageName(def.ID))
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
	stagedSHA, err := stageGGUF(s.ctx, f, size, filepath.Join(staging, modelFileName))
	if err != nil {
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
	if stagedSHA != strings.ToLower(pin.ChecksumValue) {
		fail(ErrVerify)
		return
	}
	if err := writeModelReceipt(staging, def.ID, pin, stagedSHA); err != nil {
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
	if !s.ownedDirs() {
		fail(ErrUnsafe)
		return
	}
	current, err := f.Stat()
	if err != nil || !current.Mode().IsRegular() || current.Size() != size {
		fail(ErrUnsafe)
		return
	}
	published, err := s.dls.Lstat(a.ID + ".part")
	if err != nil || !published.Mode().IsRegular() || !os.SameFile(current, published) || published.Size() != size {
		fail(ErrUnsafe)
		return
	}
	final := filepath.Join(s.modelsDir, def.ID)
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
	if !s.quickValidModel(def) {
		fail(ErrVerify)
		return
	}
	if err := s.commitReady(a.ID, def.ID, pin.Version, size); err != nil {
		fail(download.ErrStorage)
		return
	}
	s.logf("install profile=%s code=ready", def.ID)
}

func (s *Service) checkpoint() error {
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	default:
		return nil
	}
}

// Activate marks a ready profile as the default for empty-id inference.
func (s *Service) Activate(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return download.ErrStorage
	}
	def, ok := Find(s.pins, id)
	if !ok {
		return ErrNotFound
	}
	if s.lease || s.active[def.ID] {
		return ErrBusy
	}
	r, err := s.getRow("assistant_models", def.ID)
	if err != nil {
		return download.ErrStorage
	}
	if r.Status != StatusReady || !s.quickValidModel(def) {
		return ErrState
	}
	if err := s.setState("active_profile_id", def.ID); err != nil {
		return download.ErrStorage
	}
	s.logf("activate profile=%s code=ok", def.ID)
	return nil
}

// Remove deletes an installed profile and its registry row. In-flight
// inference or installation reports busy; otherwise even the active profile
// can be removed (a sole installed model stays removable) and the active id
// is cleared when it points at the removed profile. Unknown ids report
// not_found. The final directory is removed (its staged-aside .broken
// siblings are kept as repair evidence).
func (s *Service) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return download.ErrStorage
	}
	def, ok := Find(s.pins, id)
	if !ok {
		return ErrNotFound
	}
	if s.lease || s.active[def.ID] {
		return ErrBusy
	}
	r, err := s.getRow("assistant_models", def.ID)
	if err != nil {
		return download.ErrStorage
	}
	_, statErr := s.models.Lstat(def.ID)
	if !r.present && errors.Is(statErr, os.ErrNotExist) {
		return ErrNotFound
	}
	if statErr == nil {
		if !s.ownedDirs() {
			return ErrUnsafe
		}
		if err := os.RemoveAll(filepath.Join(s.modelsDir, def.ID)); err != nil {
			return download.ErrStorage
		}
	}
	if r.present {
		if _, err := s.db.Exec(`DELETE FROM assistant_models WHERE id=?`, def.ID); err != nil {
			return download.ErrStorage
		}
	}
	if activeID, err := s.getState("active_profile_id"); err != nil {
		return download.ErrStorage
	} else if activeID == def.ID {
		if err := s.setState("active_profile_id", ""); err != nil {
			return download.ErrStorage
		}
	}
	s.logf("remove profile=%s code=ok", def.ID)
	return nil
}

// StartRuntimeDownload queues the verified CPU runtime archive for the
// current platform after an explicit user action. Unsupported platforms
// report invalid_state; an already installed runtime at the same version is
// not re-downloaded.
func (s *Service) StartRuntimeDownload(m *download.Manager) (download.Snapshot, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed || m == nil {
		return download.Snapshot{}, download.ErrStorage
	}
	if s.rtPin == nil || !s.rtPin.Verified() {
		return download.Snapshot{}, ErrState
	}
	pin := *s.rtPin
	if !validChecksum(pin.ChecksumValue) {
		return download.Snapshot{}, ErrMetadata
	}
	s.mu.Lock()
	busy := s.lease || s.active[RuntimePackageID]
	s.mu.Unlock()
	if busy {
		return download.Snapshot{}, ErrBusy
	}
	r, err := s.getRow("assistant_runtime", RuntimePackageID)
	if err != nil {
		return download.Snapshot{}, download.ErrStorage
	}
	if r.Status == StatusReady && r.Version == pin.Version && s.quickValidRuntime(pin) {
		return download.Snapshot{}, ErrCollision
	}
	total := pin.TotalBytes
	expected := pin.ExpectedSize
	snap, err := m.Create(download.Descriptor{
		Provider: Provider, ProviderPackageID: RuntimePackageID,
		Title: "Asistan motoru " + pin.Version, Variant: s.platKey,
		FileName: pin.FileName, SourceURL: pin.SourceURL, TotalBytes: &total,
		ContentType: ContentTypeRuntime, Language: "tr",
		Version: pin.Version, ExpectedSize: &expected,
		ChecksumAlgorithm: "sha256", ChecksumValue: strings.ToLower(pin.ChecksumValue),
	})
	if err != nil {
		return download.Snapshot{}, err
	}
	if r.present {
		r.DownloadID = snap.ID
		if err := s.setRow("assistant_runtime", r, RuntimePackageID); err != nil {
			return download.Snapshot{}, download.ErrStorage
		}
	}
	s.logf("download runtime code=ok")
	return snap, nil
}

// InstallRuntime claims a completed runtime download and installs it
// asynchronously, preserving the archive's DLL/.so layout including safe
// symlinks. It returns true only when the runtime is already ready.
func (s *Service) InstallRuntime(m *download.Manager, downloadID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || m == nil {
		return false, download.ErrStorage
	}
	if s.rtPin == nil || !s.rtPin.Verified() {
		return false, ErrState
	}
	pin := *s.rtPin
	if strings.TrimSpace(downloadID) == "" {
		return false, ErrInvalid
	}
	if s.lease || s.active[RuntimePackageID] {
		return false, ErrBusy
	}
	if !s.ownedDirs() {
		return false, ErrUnsafe
	}
	r, err := s.getRow("assistant_runtime", RuntimePackageID)
	if err != nil {
		return false, download.ErrStorage
	}
	if r.Status == StatusReady && r.Version == pin.Version && s.quickValidRuntime(pin) {
		if r.DownloadID == downloadID {
			return true, nil
		}
		return false, ErrCollision
	}
	artifact, err := m.ClaimCompleted(downloadID, downloadID)
	if err != nil {
		return false, err
	}
	if err := checkRuntimeArtifact(pin, artifact); err != nil {
		return false, err
	}
	if artifact.TempPath != filepath.Join(s.dlDir, artifact.ID+".part") {
		return false, ErrUnsafe
	}
	fh, err := s.dls.OpenFile(artifact.ID+".part", os.O_RDONLY, 0)
	if err != nil {
		return false, ErrUnsafe
	}
	fi, err := fh.Stat()
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != pin.ExpectedSize {
		fh.Close()
		s.setRow("assistant_runtime", row{DownloadID: downloadID, Version: pin.Version, Status: StatusError, Error: ErrVerify}, RuntimePackageID)
		return false, ErrVerify
	}
	if err := s.setRow("assistant_runtime", row{DownloadID: downloadID, Version: pin.Version, Status: StatusInstalling}, RuntimePackageID); err != nil {
		fh.Close()
		return false, download.ErrStorage
	}
	s.active[RuntimePackageID] = true
	s.wg.Add(1)
	go s.workRuntime(artifact, fh, pin)
	s.logf("install runtime code=accepted")
	return false, nil
}

func checkRuntimeArtifact(pin RuntimeAcquisition, a download.Artifact) error {
	if !pin.Verified() || !validChecksum(pin.ChecksumValue) {
		return ErrMetadata
	}
	if a.Provider != Provider || a.ProviderPackageID != RuntimePackageID {
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

func (s *Service) setRuntimeError(problem *download.Error) {
	r, err := s.getRow("assistant_runtime", RuntimePackageID)
	if err != nil {
		s.logf("install runtime code=storage_error")
		return
	}
	r.Status = StatusError
	r.Error = problem
	_ = s.setRow("assistant_runtime", r, RuntimePackageID)
	s.logf("install runtime code=%s", problem.Code)
}

// workRuntime streams the archive SHA-256 once, extracts the verified bytes
// into staging (preserving safe symlinks and library layout) and publishes
// atomically under a receipt bound to the pin. The live runtime directory is
// replaced only after the staged tree validates; the previous tree is staged
// aside, never deleted.
func (s *Service) workRuntime(a download.Artifact, f *os.File, pin RuntimeAcquisition) {
	size := pin.ExpectedSize
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		delete(s.active, RuntimePackageID)
		s.mu.Unlock()
	}()
	defer f.Close()
	fail := func(problem *download.Error) { s.setRuntimeError(problem) }
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
	payloadSHA := hex.EncodeToString(h.Sum(nil))
	if payloadSHA != strings.ToLower(pin.ChecksumValue) {
		fail(ErrVerify)
		return
	}
	if err := s.ctx.Err(); err != nil {
		fail(ErrFiles)
		return
	}
	staging := filepath.Join(s.dir, stageName(RuntimePackageID))
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
	if err := s.extractRuntime(f, size, pin, staging); err != nil {
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
	files, err := manifestFiles(staging, true)
	if err != nil {
		fail(ErrVerify)
		return
	}
	binaryRel := findServerBinary(files, runtime.GOOS)
	if binaryRel == "" {
		fail(ErrVerify)
		return
	}
	if err := writeRuntimeReceipt(staging, pin, binaryRel, payloadSHA); err != nil {
		fail(ErrVerify)
		return
	}
	if st, err := os.Lstat(filepath.Join(staging, filepath.FromSlash(binaryRel))); err != nil || !st.Mode().IsRegular() {
		fail(ErrVerify)
		return
	} else if runtime.GOOS != "windows" {
		_ = os.Chmod(filepath.Join(staging, filepath.FromSlash(binaryRel)), 0o755)
	}
	if err := s.checkpoint(); err != nil {
		fail(ErrFiles)
		return
	}
	if !s.ownedDirs() {
		fail(ErrUnsafe)
		return
	}
	current, err := f.Stat()
	if err != nil || !current.Mode().IsRegular() || current.Size() != size {
		fail(ErrUnsafe)
		return
	}
	published, err := s.dls.Lstat(a.ID + ".part")
	if err != nil || !published.Mode().IsRegular() || !os.SameFile(current, published) || published.Size() != size {
		fail(ErrUnsafe)
		return
	}
	// Swap the live tree aside only after staging validates; rename the
	// staging tree into place atomically.
	previous := filepath.Join(s.dir, brokenName(RuntimePackageID))
	if err := os.Rename(s.runtimeDir, previous); err != nil {
		fail(download.ErrStorage)
		return
	}
	if err := os.Rename(staging, s.runtimeDir); err != nil {
		// Restore the previous tree; staging is cleaned by defer.
		_ = os.Rename(previous, s.runtimeDir)
		if errors.Is(err, os.ErrExist) {
			fail(ErrCollision)
		} else {
			fail(download.ErrStorage)
		}
		return
	}
	done = true
	// Reopen the confined runtime handle on the new tree: the old handle
	// still points at the staged-aside directory.
	s.mu.Lock()
	oldRT := s.rt
	fresh, rtErr := s.root.OpenRoot("content/assistant/runtime")
	if rtErr == nil {
		s.rt = fresh
		s.runtime = fresh
	}
	s.mu.Unlock()
	if rtErr != nil {
		_ = os.Rename(s.runtimeDir, staging)
		_ = os.Rename(previous, s.runtimeDir)
		fail(ErrUnsafe)
		return
	}
	_ = oldRT.Close()
	if !s.quickValidRuntime(pin) {
		fail(ErrVerify)
		return
	}
	if err := s.commitRuntimeReady(a.ID, pin.Version, size); err != nil {
		fail(download.ErrStorage)
		return
	}
	s.logf("install runtime code=ready")
}

func (s *Service) extractRuntime(f *os.File, size int64, pin RuntimeAcquisition, staging string) error {
	switch pin.Format {
	case "tar.gz":
		return extractTarGz(s.ctx, f, size, staging)
	case "zip":
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return ErrVerify
		}
		zr, err := zip.NewReader(f, size)
		if err != nil {
			return ErrVerify
		}
		members, err := planZipMembers(zr, pin)
		if err != nil {
			return err
		}
		return extractZipMembers(s.ctx, staging, members)
	default:
		return ErrMetadata
	}
}

// UseModel runs fn with a pinned ready model and runtime while holding the
// single-flight busy lease: concurrent inference or any model/runtime
// mutation is rejected with busy. An empty id uses the active profile.
// Service shutdown cancels the merged callback context promptly. The model
// path and runtime binary are pinned for the whole callback.
func (s *Service) UseModel(ctx context.Context, id string, fn func(context.Context, Model) error) error {
	if fn == nil {
		return ErrInvalid
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return download.ErrStorage
	}
	if s.lease || len(s.active) > 0 {
		s.mu.Unlock()
		return ErrBusy
	}
	s.lease = true
	s.wg.Add(1)
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.lease = false
		s.mu.Unlock()
		s.wg.Done()
	}()
	if strings.TrimSpace(id) == "" {
		active, err := s.getState("active_profile_id")
		if err != nil {
			return download.ErrStorage
		}
		id = active
	}
	def, ok := Find(s.pins, id)
	if !ok || strings.TrimSpace(id) == "" {
		return ErrNotFound
	}
	r, err := s.getRow("assistant_models", def.ID)
	if err != nil {
		return download.ErrStorage
	}
	if r.Status != StatusReady || !s.quickValidModel(def) {
		return ErrState
	}
	bin := s.effectiveBinary()
	if !binaryAvailable(bin) {
		return ErrEngine
	}
	merged, cancel := mergeDone(ctx, s.ctx)
	defer cancel()
	if err := merged.Err(); err != nil {
		if s.ctx.Err() != nil {
			return download.ErrStorage
		}
		return err
	}
	model := Model{
		ID: def.ID, Path: filepath.Join(s.modelsDir, def.ID, modelFileName),
		Binary: bin, ContextSize: def.Acquisition.ContextSize, MaxTokens: def.Acquisition.MaxTokens,
	}
	return fn(merged, model)
}

// resolveRuntimeBinary locates the llama.cpp server binary without a shell.
// An explicit override wins, then ENGELSIZ_LLAMA_BINARY (absolute paths
// only), then the installed runtime directory. No PATH lookup.
func resolveRuntimeBinary(override, runtimeDir string) string {
	if override != "" {
		return override
	}
	if dev := os.Getenv("ENGELSIZ_LLAMA_BINARY"); dev != "" {
		if filepath.IsAbs(dev) {
			return dev
		}
		return ""
	}
	name := "llama-server"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(runtimeDir, name)
}

// binaryAvailable reports whether path is a usable executable file. Relative
// custom paths are never accepted.
func binaryAvailable(path string) bool {
	if path == "" || !filepath.IsAbs(path) {
		return false
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return fi.Mode().Perm()&0o111 != 0
}

// Close interrupts in-flight inference promptly, joins install and inference
// workers and releases the owned handles.
func (s *Service) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	s.wg.Wait()
	var err error
	for _, handle := range []*os.Root{s.models, s.rt, s.dls, s.root} {
		if handle != nil {
			err = errors.Join(err, handle.Close())
		}
	}
	return err
}
