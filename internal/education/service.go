package education

import (
	"context"
	"crypto/rand"
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
	"time"

	"engelsiz/internal/download"
	"engelsiz/internal/packages"
)

// Options configures the education service. Resources carries the catalog;
// a nil slice selects DefaultResources (populated by the parent), while an
// explicitly empty slice disables acquisition. Packages is optional and only
// used for ZIM install/remove/restart-recovery; PDF/HTML tests pass nil.
type Options struct {
	Resources []Resource
	Packages  *packages.Service
	Logger    *log.Logger
}

// Service owns the durable education registry, PDF/HTML installation and ZIM
// delegation. Missing pins never fail New; only a broken catalog or storage
// does.
type Service struct {
	db           *sql.DB
	dataRoot     string
	dir          string
	downloadsDir string
	root         *os.Root
	files        *os.Root
	downloads    *os.Root
	resources    []Resource
	byID         map[string]Resource
	pkgs         *packages.Service
	logger       *log.Logger

	mu     sync.Mutex
	active map[string]bool
	closed bool

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// View is the public per-resource snapshot.
type View struct {
	Resource
	Status   string             `json:"status"`
	Download *download.Snapshot `json:"download"`
	Error    *download.Error    `json:"error"`
	OpenHref *string            `json:"openHref"`
}

// ListResponse is the GET /api/education response body.
type ListResponse struct {
	Resources []View `json:"resources"`
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
		s.logger.Printf("education "+format, args...)
	}
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
	for _, name := range []string{"content", "content/education", "downloads"} {
		if err := root.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if fi, err := root.Lstat(name); err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return nil, ErrUnsafe
		}
	}
	files, err := root.OpenRoot("content/education")
	if err != nil {
		return nil, err
	}
	downloads, err := root.OpenRoot("downloads")
	if err != nil {
		files.Close()
		return nil, err
	}
	defer func() {
		if !ok {
			files.Close()
			downloads.Close()
		}
	}()
	in := opts.Resources
	if in == nil {
		if defaultCatalogErr != nil {
			root.Close()
			return nil, defaultCatalogErr
		}
		in = DefaultResources
	}
	list := make([]Resource, 0, len(in))
	list = append(list, in...)
	if err := validateResources(list); err != nil {
		return nil, err
	}
	byID := make(map[string]Resource, len(list))
	for _, r := range list {
		byID[r.ID] = r
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{
		db: db, dataRoot: absolute, dir: filepath.Join(absolute, "content", "education"),
		downloadsDir: filepath.Join(absolute, "downloads"),
		root:         root, files: files, downloads: downloads,
		resources: list, byID: byID, pkgs: opts.Packages, logger: opts.Logger,
		active: map[string]bool{}, ctx: ctx, cancel: cancel,
	}
	if err := s.recover(); err != nil {
		cancel()
		return nil, err
	}
	ok = true
	return s, nil
}

func (s *Service) resource(id string) (Resource, bool) {
	r, ok := s.byID[id]
	return r, ok
}

func (s *Service) getRow(id string) (row, error) {
	var r row
	var size sql.NullInt64
	var code, message, downloadID, version, status sql.NullString
	err := s.db.QueryRow(`SELECT download_id,version,status,size_bytes,error_code,error_message FROM education_resources WHERE id=?`, id).
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
	_, err := s.db.Exec(`INSERT INTO education_resources(id,download_id,version,status,size_bytes,error_code,error_message,updated_at) VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET download_id=excluded.download_id,version=excluded.version,status=excluded.status,size_bytes=excluded.size_bytes,error_code=excluded.error_code,error_message=excluded.error_message,updated_at=excluded.updated_at`,
		id, nullable(r.DownloadID), r.Version, r.Status, size, code, message, stamp)
	return err
}

func (s *Service) deleteRow(id string) error {
	_, err := s.db.Exec(`DELETE FROM education_resources WHERE id=?`, id)
	return err
}

func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
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
	for name, handle := range map[string]*os.Root{"content/education": s.files, "downloads": s.downloads} {
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

// recover reconciles interrupted work without networking. Only exact known
// resource names are ever touched: staging siblings of known IDs are removed,
// crash-orphaned .deleting trash is restored or completed per the durable
// row, and unknown files are never touched. A valid PDF/HTML directory is
// adopted idempotently; vanished payloads surface as recoverable errors.
// ZIM readiness derives from the packages service: an installing row is
// adopted when the package install completed, a failed package surfaces its
// error, otherwise interrupted work surfaces as an error. Crash handling is
// intentionally explicit here rather than exact: installing rows never
// silently become installed.
func (s *Service) recover() error {
	dh, err := s.files.Open(".")
	if err != nil {
		return err
	}
	names, err := dh.ReadDir(-1)
	dh.Close()
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, e := range names {
		known[e.Name()] = true
	}
	// Narrow staging sweep: only exact-shaped ".staging-<known-id>-<16 hex>"
	// names created by our own installs are removed. Anything else —
	// foreign names, wrong shapes, unknown files — is preserved.
	for _, def := range s.resources {
		prefix := ".staging-" + def.ID + "-"
		for name := range known {
			if !isStagingName(name, prefix) {
				continue
			}
			fi, err := s.files.Lstat(name)
			if err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
				return ErrUnsafe
			}
			if err := s.files.RemoveAll(name); err != nil {
				return err
			}
			delete(known, name)
			s.logf("recovered resource id=%s code=staging_cleaned", def.ID)
		}
	}
	for _, def := range s.resources {
		if !def.Downloadable || !def.VerifiedPin() {
			continue
		}
		r, err := s.getRow(def.ID)
		if err != nil {
			return err
		}
		if def.Format == FormatZIM {
			switch r.Status {
			case "installing":
				verifiedID, verified, verr := pkgsInstalledVerified(s.pkgs, def)
				if verr != nil {
					return verr
				}
				if verified {
					// Persist the exact verified install identity, never a
					// stale row association.
					if err := s.setRow(row{DownloadID: verifiedID, Version: def.Version, Status: "installed", Size: sql.NullInt64{Int64: *def.SizeBytes, Valid: true}}, def.ID); err != nil {
						return err
					}
					s.logf("recovered resource id=%s code=ok", def.ID)
					break
				}
				pkg, hasPkg, perr := pkgsRecord(s.pkgs, r.DownloadID)
				if perr != nil {
					return perr
				}
				if hasPkg && pkg.Status == "failed" {
					problem := pkg.Error
					if problem == nil {
						problem = ErrVerify
					}
					if err := s.setRow(row{DownloadID: r.DownloadID, Version: r.Version, Status: "error", Size: r.Size, Error: problem}, def.ID); err != nil {
						return err
					}
					s.logf("recovered resource id=%s code=failed", def.ID)
				} else if hasPkg && (pkg.Status == "verifying" || pkg.Status == "installing") {
					// The delegated packages worker survived the restart and
					// is still working; keep the installing row as-is.
					s.logf("recovered resource id=%s code=installing", def.ID)
				} else if err := s.setRow(row{DownloadID: r.DownloadID, Version: r.Version, Status: "error", Size: r.Size, Error: &download.Error{Code: "interrupted", Message: "İşlem uygulama kapanınca kesildi. Tekrar deneyin."}}, def.ID); err != nil {
					return err
				} else {
					s.logf("recovered resource id=%s code=interrupted", def.ID)
				}
			case "installed":
				if s.pkgs != nil {
					_, ok, verr := pkgsInstalledVerified(s.pkgs, def)
					if verr != nil {
						return verr
					}
					if !ok {
						if err := s.setRow(row{DownloadID: r.DownloadID, Version: r.Version, Status: "error", Size: r.Size, Error: ErrFiles}, def.ID); err != nil {
							return err
						}
						s.logf("recovered resource id=%s code=artifact_unavailable", def.ID)
					}
				}
			}
			continue
		}
		// Crash-orphaned delete trash: a known "<id>.deleting" name only.
		// Row still installed means the delete never committed: restore the
		// payload (preserve content; the user can retry delete). Row gone
		// means the delete committed: drop the leftover trash.
		if _, err := s.files.Lstat(def.ID + ".deleting"); err == nil {
			if err := s.reconcileTrash(def, r); err != nil {
				return err
			}
			r, err = s.getRow(def.ID)
			if err != nil {
				return err
			}
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return ErrUnsafe
		}
		valid := validatePayload(s.files, def) == nil
		switch {
		case valid && r.Status == "installed":
			// Steady state.
		case valid:
			assoc := r.DownloadID
			if assoc == "" || s.tombstoned(assoc) {
				assoc = ""
			}
			if assoc != "" {
				if err := s.commitReady(assoc, def.ID, def.Version, *def.SizeBytes); err != nil {
					return err
				}
			} else if err := s.setRow(row{Version: def.Version, Status: "installed", Size: sql.NullInt64{Int64: *def.SizeBytes, Valid: true}}, def.ID); err != nil {
				return err
			}
			s.logf("recovered resource id=%s code=ok", def.ID)
		case !valid && r.Status == "installed":
			if err := s.setRow(row{DownloadID: r.DownloadID, Version: r.Version, Status: "error", Size: r.Size, Error: ErrFiles}, def.ID); err != nil {
				return err
			}
			s.logf("recovered resource id=%s code=artifact_unavailable", def.ID)
		case !valid && r.Status == "installing":
			if err := s.setRow(row{DownloadID: r.DownloadID, Version: r.Version, Status: "error", Size: r.Size, Error: &download.Error{Code: "interrupted", Message: "İşlem uygulama kapanınca kesildi. Tekrar deneyin."}}, def.ID); err != nil {
				return err
			}
			s.logf("recovered resource id=%s code=interrupted", def.ID)
		default:
			// Error or missing rows with invalid payloads stay as-is; the
			// explicit install path repairs them without deleting anything.
		}
	}
	return nil
}

// reconcileTrash handles one known "<id>.deleting" leftover from a crash
// during Delete. Only this exact owned name is ever touched.
func (s *Service) reconcileTrash(def Resource, r row) error {
	fi, err := s.files.Lstat(def.ID + ".deleting")
	if err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return ErrUnsafe
	}
	if _, err := s.files.Lstat(def.ID); errors.Is(err, os.ErrNotExist) && r.present && r.Status == "installed" {
		// Delete crashed between staging and row commit: restore payload.
		if !s.dirOwned() {
			return ErrUnsafe
		}
		if err := s.files.Rename(def.ID+".deleting", def.ID); err != nil {
			return err
		}
		s.logf("recovered resource id=%s code=delete_restored", def.ID)
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrUnsafe
	}
	// Delete committed (row gone) or final still present: drop leftover trash.
	// A committed delete also retires the resource transfers so the orphaned
	// completed download cannot resurrect a downloaded state.
	if err := s.tombstoneResource(def); err != nil {
		return err
	}
	if err := s.files.RemoveAll(def.ID + ".deleting"); err != nil {
		return err
	}
	s.logf("recovered resource id=%s code=trash_cleaned", def.ID)
	return nil
}

// commitReady marks the artifact consumed and the resource installed
// atomically. Persistence failures surface as storage errors.
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
	res, err = tx.Exec(`UPDATE education_resources SET download_id=?,version=?,status='installed',size_bytes=?,error_code='',error_message='',updated_at=? WHERE id=?`, downloadID, version, size, now(), id)
	if err != nil {
		// Crash between publish and first commit leaves no row to update;
		// insert the adopted row instead.
		if _, err := tx.Exec(`INSERT INTO education_resources(id,download_id,version,status,size_bytes,updated_at) VALUES(?,?,?,?,?,?)`, id, downloadID, version, "installed", size, now()); err != nil {
			return download.ErrStorage
		}
	} else if n, err := res.RowsAffected(); err != nil || n != 1 {
		if _, err := tx.Exec(`INSERT INTO education_resources(id,download_id,version,status,size_bytes,updated_at) VALUES(?,?,?,?,?,?)`, id, downloadID, version, "installed", size, now()); err != nil {
			return download.ErrStorage
		}
	}
	if err := tx.Commit(); err != nil {
		return download.ErrStorage
	}
	return nil
}

// pkgMatchesDef reports whether an installed package record is exactly this
// resource's verified payload: matching identity, namespaced variant, pinned
// version and checksum. A bare download-ID match is never enough: a foreign
// (e.g. Wikipedia) install must not satisfy an educational resource.
func pkgMatchesDef(p packages.Package, def Resource) bool {
	return p.Provider == ProviderZIM && p.ProviderPackageID == def.ID &&
		p.ContentType == ContentTypeZIM && p.Variant == VariantOf(def.ID) &&
		p.Version == def.Version && p.Checksum.Algorithm == "sha256" &&
		strings.ToLower(p.Checksum.Value) == strings_Lower(def.SHA256)
}

// pkgsRecord returns the package record for one download ID, if any.
// Storage failures are bubbled so snapshots fail closed instead of
// misreporting status.
func pkgsRecord(pkgs *packages.Service, downloadID string) (packages.Package, bool, error) {
	if pkgs == nil || downloadID == "" {
		return packages.Package{}, false, nil
	}
	items, err := pkgs.List("")
	if err != nil {
		return packages.Package{}, false, err
	}
	for _, p := range items {
		if p.ID == downloadID {
			return p, true, nil
		}
	}
	return packages.Package{}, false, nil
}

// pkgsInstalledVerified returns the associated download ID when the packages
// service holds a fully verified install of def. Storage failures bubble.
func pkgsInstalledVerified(pkgs *packages.Service, def Resource) (string, bool, error) {
	if pkgs == nil || def.Format != FormatZIM {
		return "", false, nil
	}
	items, err := pkgs.List("")
	if err != nil {
		return "", false, err
	}
	for _, p := range items {
		if p.Status == "installed" && pkgMatchesDef(p, def) {
			return p.ID, true, nil
		}
	}
	return "", false, nil
}

// tombstoned reports whether a download ID was retired by Delete. Tombstoned
// transfers never resurrect "downloaded" status; only a fresh user download
// creates a new association. Storage failures fail closed (skip).
func (s *Service) tombstoned(id string) bool {
	if id == "" {
		return true
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM education_tombstones WHERE download_id=?`, id).Scan(&n); err != nil {
		return true
	}
	return n != 0
}

// tombstoneResource retires every live transfer of def so Delete cannot
// resurrect "downloaded" from an old completed/consumed download. Rows are
// fully consumed and closed before writing: the database pool is a single
// connection, so an open Rows would deadlock any subsequent Exec. Query,
// scan, iteration and insert failures are returned: callers fail the Delete
// instead of reporting success while stale transfers stay live.
func (s *Service) tombstoneResource(def Resource) error {
	rows, err := s.db.Query(`SELECT id FROM downloads WHERE provider_package_id=? AND provider IN ('education','kiwix') AND status != 'cancelled'`, def.ID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, id := range ids {
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO education_tombstones(download_id,resource_id) VALUES(?,?)`, id, def.ID); err != nil {
			return err
		}
	}
	return nil
}

// transferConsumed reports whether a transfer's bytes were already consumed
// by an install. Unknown rows fail closed as unconsumed: the caller then
// refuses a duplicate rather than risking a redundant transfer.
func (s *Service) transferConsumed(id string) bool {
	var state string
	if err := s.db.QueryRow(`SELECT artifact_state FROM downloads WHERE id=?`, id).Scan(&state); err != nil {
		return false
	}
	return state == "consumed"
}

// findDownload resolves the resource's download view. The durable latest
// association wins; otherwise the newest live manager row for this resource is
// shown so install can use the user's own completed download. Tombstoned
// (deleted) transfers are never resurrected. Failures surface as null, never
// as errors.
func (s *Service) findDownload(def Resource, associated string, m *download.Manager) *download.Snapshot {
	if m == nil {
		return nil
	}
	if associated != "" && !s.tombstoned(associated) {
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
		if item.ProviderPackageID != def.ID || item.Status == "cancelled" || s.tombstoned(item.ID) {
			continue
		}
		if def.Format == FormatZIM && item.Provider != ProviderZIM {
			continue
		}
		if def.Format != FormatZIM && item.Provider != ProviderFiles {
			continue
		}
		dup := item
		return &dup
	}
	return nil
}

func viewMeta(def Resource) View {
	v := View{Resource: def, Status: StatusMissing}
	if v.SizeBytes == nil && def.SizeBytes != nil {
		n := *def.SizeBytes
		v.SizeBytes = &n
	}
	return v
}

func (s *Service) snapshotWith(def Resource, m *download.Manager, pkgs *packages.Service, installing bool) (View, error) {
	out := viewMeta(def)
	if !def.Downloadable || !def.VerifiedPin() {
		out.Status = StatusReference
		return out, nil
	}
	r, err := s.getRow(def.ID)
	if err != nil {
		return View{}, download.ErrStorage
	}
	if r.Version != "" {
		out.Version = r.Version
	}
	if r.Size.Valid {
		n := r.Size.Int64
		out.SizeBytes = &n
	}
	dl := s.findDownload(def, r.DownloadID, m)
	out.Download = dl
	if def.Format == FormatZIM {
		assoc := r.DownloadID
		if assoc == "" && dl != nil {
			assoc = dl.ID
		}
		pkg, hasPkg, perr := pkgsRecord(pkgs, assoc)
		if perr != nil {
			return View{}, perr
		}
		verifiedID, verified, verr := pkgsInstalledVerified(pkgs, def)
		if verr != nil {
			return View{}, verr
		}
		if verified && dl == nil && m != nil {
			if snap, err := m.Get(verifiedID); err == nil {
				dup := snap
				out.Download = &dup
			}
		}
		// Active transfers report downloading even though the durable row
		// says installing: no education worker exists for ZIMs, and the row
		// is set at download time. Package worker states take precedence
		// over the row once delegation has happened.
		switch {
		case verified:
			out.Status = StatusInstalled
		case hasPkg && (pkg.Status == "verifying" || pkg.Status == "installing"):
			out.Status = StatusInstalling
		case hasPkg && pkg.Status == "failed":
			out.Status = StatusFailed
			out.Error = pkg.Error
			if out.Error == nil {
				out.Error = ErrFiles
			}
		case dl != nil && (dl.Status == "queued" || dl.Status == "downloading" || dl.Status == "paused"):
			out.Status = StatusDownloading
		case dl != nil && dl.Status == "completed":
			out.Status = StatusDownloaded
		case r.Status == "error":
			out.Status = StatusFailed
			out.Error = r.Error
			if out.Error == nil {
				out.Error = ErrFiles
			}
		case installing || r.Status == "installing":
			out.Status = StatusInstalling
		case dl != nil:
			out.Status = StatusFailed
		default:
			out.Status = StatusMissing
		}
		return out, nil
	}
	installed := r.Status == "installed" && s.quickValid(def)
	switch {
	case installing:
		out.Status = StatusInstalling
	case installed:
		out.Status = StatusInstalled
		href := "/education/content/" + def.ID
		out.OpenHref = &href
	case dl != nil && (dl.Status == "queued" || dl.Status == "downloading" || dl.Status == "paused"):
		out.Status = StatusDownloading
	case r.Status == "error":
		// A stored failure always wins over a completed transfer: only an
		// explicit new download (which clears the error) moves the view to
		// downloaded.
		out.Status = StatusFailed
		out.Error = r.Error
		if out.Error == nil {
			out.Error = ErrFiles
		}
	case r.Status == "installed" && dl != nil && dl.Status == "completed" && dl.ID != r.DownloadID:
		// Fresh bytes supersede a broken install; the install's own
		// consumed transfer below never resurrects installed content.
		out.Status = StatusDownloaded
	case r.Status == "installed":
		out.Status = StatusFailed
		out.Error = ErrFiles
	case dl != nil && dl.Status == "completed":
		out.Status = StatusDownloaded
	case dl != nil:
		out.Status = StatusFailed
	default:
		out.Status = StatusMissing
	}
	return out, nil
}

// List is read-only: it never writes the registry and never starts downloads.
func (s *Service) List(m *download.Manager, pkgs *packages.Service) (ListResponse, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return ListResponse{}, download.ErrStorage
	}
	out := make([]View, 0, len(s.resources))
	for _, def := range s.resources {
		s.mu.Lock()
		installing := s.active[def.ID]
		s.mu.Unlock()
		v, err := s.snapshotWith(def, m, pkgs, installing)
		if err != nil {
			return ListResponse{}, err
		}
		out = append(out, v)
	}
	return ListResponse{Resources: out}, nil
}

// dirOwned verifies the content/education directory itself is still the owned
// directory (not a symlink, same file as the confined root handle). It gates
// every absolute-path mutation.
func (s *Service) dirOwned() bool {
	cur, err := os.Lstat(s.dir)
	if err != nil || !cur.IsDir() || cur.Mode()&os.ModeSymlink != 0 {
		return false
	}
	orig, err := s.files.Stat(".")
	if err != nil || !os.SameFile(cur, orig) {
		return false
	}
	return true
}

// StartDownload creates a generic download for an explicit user action and
// returns the updated resource view. A resource that is already installed at
// the same pinned version is not re-downloaded.
func (s *Service) StartDownload(m *download.Manager, pkgs *packages.Service, id string) (View, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed || m == nil {
		return View{}, download.ErrStorage
	}
	def, ok := s.resource(id)
	if !ok {
		return View{}, ErrNotFound
	}
	if !def.Downloadable || !def.VerifiedPin() {
		return View{}, ErrMetadata
	}
	r, err := s.getRow(def.ID)
	if err != nil {
		return View{}, download.ErrStorage
	}
	if def.Format == FormatZIM {
		if _, ok, verr := pkgsInstalledVerified(pkgs, def); verr != nil {
			return View{}, verr
		} else if ok {
			return View{}, ErrCollision
		}
		// A delegated packages worker still verifying/installing the current
		// association owns the row: a new download must not overwrite it
		// (Delete of the new id would orphan the served install).
		if pkg, hasPkg, perr := pkgsRecord(pkgs, r.DownloadID); perr != nil {
			return View{}, perr
		} else if hasPkg && (pkg.Status == "verifying" || pkg.Status == "installing") {
			return View{}, ErrCollision
		}
	} else if r.Status == "installed" && r.Version == def.Version && s.quickValid(def) {
		return View{}, ErrCollision
	}
	// A completed but unconsumed latest transfer is reused, not duplicated:
	// starting another download for it is a conflict. Explicit repair of a
	// broken install or a failed row stays allowed. The generic manager only
	// blocks unfinished transfers, so this resource-level check closes the
	// completed-before-install duplication window.
	if latest := s.findDownload(def, r.DownloadID, m); latest != nil && latest.Status == "completed" && !s.transferConsumed(latest.ID) {
		repair := r.Status == "error"
		if !repair && r.Status == "installed" {
			if def.Format == FormatZIM {
				_, verified, verr := pkgsInstalledVerified(pkgs, def)
				if verr != nil {
					return View{}, verr
				}
				repair = !verified
			} else {
				repair = !s.quickValid(def)
			}
		}
		if !repair {
			return View{}, download.ErrExists
		}
	}
	total := *def.SizeBytes
	expected := *def.SizeBytes
	provider := ProviderFiles
	contentType := ContentTypeFiles
	if def.Format == FormatZIM {
		provider = ProviderZIM
		contentType = ContentTypeZIM
	}
	snap, err := m.Create(download.Descriptor{
		Provider: provider, ProviderPackageID: def.ID,
		Title: def.Title, Variant: VariantOf(def.ID),
		FileName: def.Filename, SourceURL: def.DownloadURL, TotalBytes: &total,
		ContentType: contentType, Language: def.Language,
		Version: def.Version, ExpectedSize: &expected,
		ChecksumAlgorithm: "sha256", ChecksumValue: strings_Lower(def.SHA256),
	})
	if err != nil {
		return View{}, err
	}
	// Every explicit download persists the latest association and clears any
	// stored error: the new transfer supersedes failed history, and the row
	// always names a live (non-tombstoned) download.
	if err := s.setRow(row{DownloadID: snap.ID, Version: def.Version, Status: "installing"}, def.ID); err != nil {
		return View{}, download.ErrStorage
	}
	s.logf("download resource=%s code=ok", def.ID)
	return s.snapshotWith(def, m, pkgs, false)
}

// Install claims a completed download and installs it. An empty downloadID
// uses the latest associated download. It returns installed=true only when
// the resource is already installed.
func (s *Service) Install(m *download.Manager, pkgs *packages.Service, id, downloadID string) (View, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || m == nil {
		return View{}, false, download.ErrStorage
	}
	def, ok := s.resource(id)
	if !ok {
		return View{}, false, ErrNotFound
	}
	if !def.Downloadable || !def.VerifiedPin() {
		return View{}, false, ErrMetadata
	}
	if !s.ownedDirs() {
		return View{}, false, ErrUnsafe
	}
	r, err := s.getRow(def.ID)
	if err != nil {
		return View{}, false, download.ErrStorage
	}
	if strings.TrimSpace(downloadID) == "" {
		s.mu.Unlock()
		dl := s.findDownload(def, r.DownloadID, m)
		s.mu.Lock()
		if s.closed {
			return View{}, false, download.ErrStorage
		}
		if dl == nil {
			return View{}, false, ErrInvalid
		}
		downloadID = dl.ID
		r, err = s.getRow(def.ID)
		if err != nil {
			return View{}, false, download.ErrStorage
		}
	}
	if def.Format == FormatZIM {
		return s.installZIM(m, pkgs, def, r, downloadID)
	}
	_, statErr := s.files.Lstat(def.ID)
	finalExists := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return View{}, false, ErrUnsafe
	}
	quick := finalExists && s.quickValid(def)
	if r.Status == "installed" && quick {
		if r.DownloadID == downloadID {
			v, err := s.snapshotWith(def, m, pkgs, false)
			return v, true, err
		}
		return View{}, false, ErrCollision
	}
	if s.active[def.ID] {
		v, err := s.snapshotWith(def, m, pkgs, true)
		return v, false, err
	}
	artifact, err := m.ClaimCompleted(downloadID, downloadID)
	if err != nil {
		return View{}, false, err
	}
	if err := s.checkFileArtifact(def, artifact); err != nil {
		return View{}, false, err
	}
	if artifact.TempPath != filepath.Join(s.downloadsDir, artifact.ID+".part") {
		return View{}, false, ErrUnsafe
	}
	fh, err := s.downloads.OpenFile(artifact.ID+".part", os.O_RDONLY, 0)
	if err != nil {
		return View{}, false, ErrUnsafe
	}
	fi, err := fh.Stat()
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != *def.SizeBytes {
		fh.Close()
		_ = s.setRow(row{DownloadID: downloadID, Version: def.Version, Status: "error", Error: ErrVerify}, def.ID)
		return View{}, false, ErrVerify
	}
	if finalExists && quick && r.Status != "error" {
		fh.Close()
		return View{}, false, ErrCollision
	}
	if finalExists {
		if !s.dirOwned() {
			fh.Close()
			return View{}, false, ErrUnsafe
		}
		if err := s.files.Rename(def.ID, brokenName(def.ID)); err != nil {
			fh.Close()
			return View{}, false, ErrCollision
		}
		s.logf("install resource=%s code=repaired", def.ID)
	}
	if err := s.setRow(row{DownloadID: downloadID, Version: def.Version, Status: "installing"}, def.ID); err != nil {
		fh.Close()
		return View{}, false, download.ErrStorage
	}
	s.active[def.ID] = true
	s.wg.Add(1)
	go s.work(artifact, fh, def)
	s.logf("install resource=%s code=accepted", def.ID)
	v, err := s.snapshotWith(def, m, pkgs, true)
	return v, false, err
}

// installZIM validates the completed transfer against the catalog pin FIRST
// and only then branches on install state. Metadata validation precedes every
// idempotency shortcut, so a foreign (e.g. Wikipedia) install can never be
// associated with an educational id. Callers hold s.mu.
func (s *Service) installZIM(m *download.Manager, pkgs *packages.Service, def Resource, r row, downloadID string) (View, bool, error) {
	if pkgs == nil {
		return View{}, false, download.ErrStorage
	}
	if err := s.checkZIMDownload(def, downloadID); err != nil {
		return View{}, false, err
	}
	// A verified install already bound to a different download never
	// silently adopts the requested one: installing over it is a collision,
	// never an association with an unrelated or older job.
	verifiedID, verified, verr := pkgsInstalledVerified(pkgs, def)
	if verr != nil {
		return View{}, false, verr
	}
	if verified && verifiedID != downloadID {
		return View{}, false, ErrCollision
	}
	pkg, hasPkg, perr := pkgsRecord(pkgs, downloadID)
	if perr != nil {
		return View{}, false, perr
	}
	if hasPkg {
		if !pkgMatchesDef(pkg, def) {
			return View{}, false, ErrMetadata
		}
		switch pkg.Status {
		case "installed":
			if err := s.setRow(row{DownloadID: downloadID, Version: def.Version, Status: "installed", Size: sql.NullInt64{Int64: *def.SizeBytes, Valid: true}}, def.ID); err != nil {
				return View{}, false, download.ErrStorage
			}
			v, err := s.snapshotWith(def, m, pkgs, false)
			return v, true, err
		case "failed":
			problem := pkg.Error
			if problem == nil {
				problem = ErrVerify
			}
			_ = s.setRow(row{DownloadID: downloadID, Version: def.Version, Status: "error", Size: r.Size, Error: problem}, def.ID)
			return View{}, false, problem
		}
	}
	pkg, installed, err := pkgs.Install(m, downloadID)
	if err != nil {
		return View{}, false, err
	}
	if !pkgMatchesDef(pkg, def) {
		return View{}, false, ErrMetadata
	}
	if installed {
		if err := s.setRow(row{DownloadID: downloadID, Version: def.Version, Status: "installed", Size: sql.NullInt64{Int64: *def.SizeBytes, Valid: true}}, def.ID); err != nil {
			return View{}, false, download.ErrStorage
		}
		v, err := s.snapshotWith(def, m, pkgs, false)
		return v, true, err
	}
	if err := s.setRow(row{DownloadID: downloadID, Version: def.Version, Status: "installing"}, def.ID); err != nil {
		return View{}, false, download.ErrStorage
	}
	s.logf("install resource=%s code=accepted", def.ID)
	v, err := s.snapshotWith(def, m, pkgs, true)
	return v, false, err
}

// checkZIMDownload validates trusted manager/DB metadata against the catalog
// pin before delegating. No client-supplied paths are used.
func (s *Service) checkZIMDownload(def Resource, downloadID string) error {
	var provider, packageID, contentType, language, version, variant, checksumAlg, checksum, temp, status string
	var expected sql.NullInt64
	var total sql.NullInt64
	err := s.db.QueryRow(`SELECT provider,provider_package_id,content_type,language,version,variant,expected_size,checksum_algorithm,checksum_value,temp_path,total_bytes,status FROM downloads WHERE id=?`, downloadID).
		Scan(&provider, &packageID, &contentType, &language, &version, &variant, &expected, &checksumAlg, &checksum, &temp, &total, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return download.ErrStorage
	}
	if status != "completed" {
		return download.ErrState
	}
	if provider != ProviderZIM || packageID != def.ID || contentType != ContentTypeZIM || variant != VariantOf(def.ID) {
		return ErrMetadata
	}
	if language != def.Language || version != def.Version {
		return ErrMetadata
	}
	if !expected.Valid || expected.Int64 != *def.SizeBytes || !total.Valid || total.Int64 != *def.SizeBytes {
		return ErrMetadata
	}
	if strings.ToLower(checksumAlg) != "sha256" || strings.ToLower(strings.TrimSpace(checksum)) != strings_Lower(def.SHA256) {
		return ErrMetadata
	}
	if temp != filepath.Join(s.downloadsDir, downloadID+".part") {
		return ErrMetadata
	}
	return nil
}

// checkFileArtifact validates trusted manager metadata against verified pins.
func (s *Service) checkFileArtifact(def Resource, a download.Artifact) error {
	if !def.VerifiedPin() {
		return ErrMetadata
	}
	if a.Provider != ProviderFiles || a.ProviderPackageID != def.ID {
		return ErrMetadata
	}
	if a.Variant != VariantOf(def.ID) || a.ContentType != ContentTypeFiles {
		return ErrMetadata
	}
	if strings.ToLower(a.ChecksumAlgorithm) != "sha256" || strings.ToLower(strings.TrimSpace(a.ChecksumValue)) != strings_Lower(def.SHA256) {
		return ErrChecksum
	}
	if a.ExpectedSize == nil || *a.ExpectedSize != *def.SizeBytes || *def.SizeBytes <= 0 {
		return ErrMetadata
	}
	if a.Version != def.Version {
		return ErrMetadata
	}
	return nil
}

func (s *Service) setError(id string, problem *download.Error) {
	r, err := s.getRow(id)
	if err != nil {
		s.logf("install resource=%s code=storage_error", id)
		return
	}
	r.Status = "error"
	r.Error = problem
	_ = s.setRow(r, id)
	s.logf("install resource=%s code=%s", id, problem.Code)
}

// work verifies, stages, publishes and commits one PDF/HTML file. The
// artifact file stays open from hashing through the pre-publish ownership
// recheck, so the bytes verified are the bytes installed. All staging happens
// through confined roots; only exact owned names are created or renamed. The
// staged copy is hashed against the pin before the receipt is written, so the
// recorded bytes can never differ from the verified bytes.
func (s *Service) work(a download.Artifact, f *os.File, def Resource) {
	size := *def.SizeBytes
	expect := strings_Lower(def.SHA256)
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
	head := make([]byte, 8192)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		fail(ErrVerify)
		return
	}
	hn, _ := io.ReadFull(f, head)
	head = head[:hn]
	if err := checkMagic(def.Format, head); err != nil {
		fail(err)
		return
	}
	if err := s.checkpoint(); err != nil {
		fail(ErrFiles)
		return
	}
	if !s.dirOwned() {
		fail(ErrUnsafe)
		return
	}
	staging := stageName(def.ID)
	if err := s.files.Mkdir(staging, 0o700); err != nil {
		fail(download.ErrStorage)
		return
	}
	done := false
	defer func() {
		if !done {
			_ = s.files.RemoveAll(staging)
		}
	}()
	st, err := s.files.OpenRoot(staging)
	if err != nil {
		fail(download.ErrStorage)
		return
	}
	copyStaged := func() error {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		w, err := st.OpenFile(def.Filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return ErrVerify
		}
		h := sha256.New()
		buf := make([]byte, 256*1024)
		var written int64
		for written <= size {
			if err := s.ctx.Err(); err != nil {
				w.Close()
				return err
			}
			n, readErr := f.Read(buf)
			if n > 0 {
				written += int64(n)
				h.Write(buf[:n])
				if _, err := w.Write(buf[:n]); err != nil {
					w.Close()
					return ErrVerify
				}
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				w.Close()
				return ErrVerify
			}
		}
		closeErr := w.Close()
		if closeErr != nil || written != size {
			return ErrVerify
		}
		// Bind the staged bytes to the pin now: a copy that differs from the
		// hashed artifact bytes can never become the installed payload.
		if hex.EncodeToString(h.Sum(nil)) != expect {
			return ErrVerify
		}
		return nil
	}
	if err := copyStaged(); err != nil {
		st.Close()
		var problem *download.Error
		if errors.As(err, &problem) {
			fail(problem)
		} else {
			fail(ErrVerify)
		}
		return
	}
	if err := writeReceipt(st, def); err != nil {
		st.Close()
		var problem *download.Error
		if errors.As(err, &problem) {
			fail(problem)
		} else {
			fail(ErrVerify)
		}
		return
	}
	st.Close()
	if err := s.checkpoint(); err != nil {
		fail(ErrFiles)
		return
	}
	if !s.ownedDirs() || !s.dirOwned() {
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
	if _, err := s.files.Lstat(def.ID); err == nil {
		fail(ErrCollision)
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		fail(ErrUnsafe)
		return
	}
	if err := s.files.Rename(staging, def.ID); err != nil {
		if errors.Is(err, os.ErrExist) {
			fail(ErrCollision)
		} else {
			fail(download.ErrStorage)
		}
		return
	}
	done = true
	if err := validatePayload(s.files, def); err != nil {
		fail(ErrVerify)
		return
	}
	if err := s.commitReady(a.ID, def.ID, def.Version, size); err != nil {
		fail(download.ErrStorage)
		return
	}
	s.logf("install resource=%s code=ready", def.ID)
}

func (s *Service) checkpoint() error {
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	default:
		return nil
	}
}

// checkMagic enforces format magic: %PDF- for PDFs and an HTML document open
// for HTML. It deliberately does not reject inline http strings: audited PhET
// documents carry license links plus optional GTM/telemetry references, and
// the network is fail-closed via serve-time CSP instead. Eval-dependent
// scripts are a serve-time CSP matter (browser probe: energy 1.4.26 fails
// without 'unsafe-eval'); install validation never executes document code.
func checkMagic(format string, head []byte) *download.Error {
	switch format {
	case FormatPDF:
		if len(head) >= 5 && string(head[:5]) == "%PDF-" {
			return nil
		}
		return ErrVerify
	case FormatHTML:
		trimmed := head
		for len(trimmed) > 0 && (trimmed[0] == ' ' || trimmed[0] == '\t' || trimmed[0] == '\n' || trimmed[0] == '\r') {
			trimmed = trimmed[1:]
		}
		if len(trimmed) >= 3 && trimmed[0] == 0xEF && trimmed[1] == 0xBB && trimmed[2] == 0xBF {
			trimmed = trimmed[3:]
		}
		lower := make([]byte, len(trimmed))
		for i, c := range trimmed {
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			lower[i] = c
		}
		if startsWith(lower, "<!doctype html") || startsWith(lower, "<html") {
			return nil
		}
		return ErrVerify
	default:
		return ErrMetadata
	}
}

func startsWith(b []byte, prefix string) bool {
	if len(b) < len(prefix) {
		return false
	}
	return string(b[:len(prefix)]) == prefix
}

// Delete removes installed content and its durable row, preserving unrelated
// files. Removing an in-progress install is refused. Deleting a missing
// resource is idempotent and returns its current view.
func (s *Service) Delete(m *download.Manager, pkgs *packages.Service, id string) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return View{}, download.ErrStorage
	}
	def, ok := s.resource(id)
	if !ok {
		return View{}, ErrNotFound
	}
	if !s.ownedDirs() {
		return View{}, ErrUnsafe
	}
	if s.active[def.ID] {
		return View{}, ErrState
	}
	r, err := s.getRow(def.ID)
	if err != nil {
		return View{}, download.ErrStorage
	}
	if def.Format == FormatZIM {
		// Without the packages service a ZIM delete cannot remove the
		// installed payload: fail instead of clearing the row while the
		// content is still served.
		if pkgs == nil {
			return View{}, download.ErrStorage
		}
		target := r.DownloadID
		if target == "" || s.tombstoned(target) {
			id, ok, verr := pkgsInstalledVerified(pkgs, def)
			if verr != nil {
				return View{}, verr
			}
			if ok {
				target = id
			} else if dl := s.findDownload(def, "", m); dl != nil {
				target = dl.ID
			} else {
				target = ""
			}
		}
		if target != "" {
			if err := pkgs.RemoveEducation(target); err != nil {
				var problem *download.Error
				if errors.As(err, &problem) && problem.Code == "not_found" {
					// Nothing installed under that transfer; still clear ours.
				} else {
					return View{}, err
				}
			}
		}
		if err := s.tombstoneResource(def); err != nil {
			return View{}, download.ErrStorage
		}
		if err := s.deleteRow(def.ID); err != nil {
			return View{}, download.ErrStorage
		}
		s.logf("delete resource=%s code=ok", def.ID)
		return s.snapshotWith(def, m, pkgs, false)
	}
	if _, err := s.files.Lstat(def.ID); err != nil && !errors.Is(err, os.ErrNotExist) {
		return View{}, ErrUnsafe
	} else if err == nil {
		// Retire transfers before moving anything: if retirement fails,
		// nothing has moved and Delete reports storage_error instead of
		// success while stale transfers stay live.
		if err := s.tombstoneResource(def); err != nil {
			return View{}, download.ErrStorage
		}
		// Stale trash from a crashed delete is handled next: a leftover
		// "<id>.deleting" beside a live final is removed (exact known name
		// only, after verifying it is a plain directory).
		if _, err := s.files.Lstat(def.ID + ".deleting"); err == nil {
			if !s.dirOwned() {
				return View{}, ErrUnsafe
			}
			trashFi, err := s.files.Lstat(def.ID + ".deleting")
			if err != nil || !trashFi.IsDir() || trashFi.Mode()&os.ModeSymlink != 0 {
				return View{}, ErrUnsafe
			}
			if err := s.files.RemoveAll(def.ID + ".deleting"); err != nil {
				return View{}, download.ErrStorage
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return View{}, ErrUnsafe
		}
		if !s.dirOwned() {
			return View{}, ErrUnsafe
		}
		if err := s.files.Rename(def.ID, def.ID+".deleting"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return View{}, ErrUnsafe
		}
		if err := s.deleteRow(def.ID); err != nil {
			_ = s.files.Rename(def.ID+".deleting", def.ID)
			return View{}, download.ErrStorage
		}
		if err := s.files.RemoveAll(def.ID + ".deleting"); err != nil {
			return View{}, download.ErrStorage
		}
	} else {
		// No payload on disk: still retire transfers so nothing resurrects.
		if err := s.tombstoneResource(def); err != nil {
			return View{}, download.ErrStorage
		}
		if r.present {
			if err := s.deleteRow(def.ID); err != nil {
				return View{}, download.ErrStorage
			}
		}
	}
	s.logf("delete resource=%s code=ok", def.ID)
	return s.snapshotWith(def, m, pkgs, false)
}

// Open returns the installed PDF/HTML payload for content serving. ZIM files
// are never served here; they go through the existing /content proxy.
func (s *Service) Open(id string) (*os.File, os.FileInfo, string, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil, nil, "", download.ErrStorage
	}
	def, ok := s.resource(id)
	if !ok || (def.Format != FormatPDF && def.Format != FormatHTML) {
		return nil, nil, "", ErrNotFound
	}
	r, err := s.getRow(def.ID)
	if err != nil {
		return nil, nil, "", download.ErrStorage
	}
	if r.Status != "installed" || !s.quickValid(def) {
		return nil, nil, "", ErrNotFound
	}
	dir, err := s.files.Lstat(def.ID)
	if err != nil || !dir.IsDir() || dir.Mode()&os.ModeSymlink != 0 {
		return nil, nil, "", ErrNotFound
	}
	name := def.ID + "/" + def.Filename
	before, err := s.files.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Size() != *def.SizeBytes {
		return nil, nil, "", ErrNotFound
	}
	f, err := s.files.Open(name)
	if err != nil {
		return nil, nil, "", ErrNotFound
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() || !os.SameFile(before, fi) || fi.Size() != before.Size() {
		f.Close()
		return nil, nil, "", ErrNotFound
	}
	mime := "application/pdf"
	if def.Format == FormatHTML {
		mime = "text/html; charset=utf-8"
	}
	return f, fi, mime, nil
}

// isStagingName reports whether name is exactly ".staging-<id>-<16 hex>".
// Only our own stageName output matches; arbitrary suffixes are preserved.
func isStagingName(name, prefix string) bool {
	rest, ok := strings.CutPrefix(name, prefix)
	if !ok || len(rest) != 16 {
		return false
	}
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// stageName makes an unpredictable staging sibling for atomic publish.
func stageName(id string) string {
	var nonce [8]byte
	_, _ = rand.Read(nonce[:])
	return ".staging-" + id + "-" + hex.EncodeToString(nonce[:])
}

// brokenName stages a corrupt directory aside for explicit repair. It renames
// within the same parent (no deletion, no cross-directory moves).
func brokenName(id string) string {
	var nonce [8]byte
	_, _ = rand.Read(nonce[:])
	return id + ".broken-" + hex.EncodeToString(nonce[:])
}

// Close interrupts in-flight installs, joins workers, then releases handles.
// Workers are always joined before roots close.
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
	for _, handle := range []*os.Root{s.files, s.downloads, s.root} {
		if handle != nil {
			err = errors.Join(err, handle.Close())
		}
	}
	return err
}
