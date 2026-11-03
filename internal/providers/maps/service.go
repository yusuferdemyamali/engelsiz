package maps

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

var (
	ErrNotFound = errors.New("map not found")
	ErrState    = errors.New("map operation is not allowed in current state")
)

type Region struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Bounds    [4]float64 `json:"bounds"`
	MaxZoom   int        `json:"maxZoom"`
	Version   string     `json:"version"`
	Status    string     `json:"status"`
	SizeBytes *int64     `json:"sizeBytes,omitempty"`
	Error     string     `json:"error,omitempty"`
}

type Options struct {
	Source     string
	Binary     string
	HTTPClient *http.Client
	// Run is injectable for deterministic lifecycle tests.
	Run func(context.Context, string, ...string) error
}
type Service struct {
	db                  *sql.DB
	dir, source, binary string
	client              *http.Client
	run                 func(context.Context, string, ...string) error
	mu                  sync.Mutex
	cancel              context.CancelFunc
	done                chan struct{}
	closed              bool
}

func New(db *sql.DB, dataRoot string, opts Options) (*Service, error) {
	dir := filepath.Join(dataRoot, "content", "maps")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("invalid maps directory")
	}
	source := opts.Source
	if source == "" {
		source = DefaultSource
	}
	u, err := url.Parse(source)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, fmt.Errorf("invalid maps source configuration")
	}
	run := opts.Run
	if run == nil {
		run = func(ctx context.Context, binary string, args ...string) error {
			cmd := exec.CommandContext(ctx, binary, args...)
			return cmd.Run()
		}
	}
	s := &Service{db: db, dir: dir, source: source, binary: opts.Binary, client: opts.HTTPClient, run: run}
	for _, r := range catalog {
		// An interrupted extraction cannot be safely resumed; neither its temporary
		// file nor an unrecorded final file may be exposed as installed.
		if err := os.Remove(s.part(r.ID)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		var status string
		err := db.QueryRow(`SELECT status FROM map_packages WHERE id=?`, r.ID).Scan(&status)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		trash := s.path(r.ID) + ".deleting"
		if status == "ready" {
			if _, e := os.Lstat(s.path(r.ID)); errors.Is(e, os.ErrNotExist) {
				if e = os.Rename(trash, s.path(r.ID)); e != nil && !errors.Is(e, os.ErrNotExist) {
					return nil, e
				}
			}
		} else if e := os.Remove(trash); e != nil && !errors.Is(e, os.ErrNotExist) {
			return nil, e
		}
		if status != "ready" {
			if err := os.Remove(s.path(r.ID)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			if status != "" && status != "error" {
				if err := s.set(r, "error", nil, "İşlem uygulama kapanınca kesildi. Tekrar deneyin."); err != nil {
					return nil, err
				}
			}
		} else if fi, err := os.Lstat(s.path(r.ID)); err != nil || !fi.Mode().IsRegular() {
			if err := s.set(r, "error", nil, "Harita dosyası bulunamadı. Tekrar indirin."); err != nil {
				return nil, err
			}
		}
	}
	return s, nil
}
func (s *Service) part(id string) string { return filepath.Join(s.dir, id+".part") }
func (s *Service) path(id string) string { return filepath.Join(s.dir, id+".pmtiles") }
func find(id string) (Region, error) {
	for _, r := range catalog {
		if r.ID == id {
			return r, nil
		}
	}
	return Region{}, ErrNotFound
}
func (s *Service) set(r Region, status string, size *int64, message string) error {
	_, err := s.db.Exec(`INSERT INTO map_packages(id,title,version,status,size_bytes,error) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET title=excluded.title,version=excluded.version,status=excluded.status,size_bytes=excluded.size_bytes,error=excluded.error,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')`, r.ID, r.Title, r.Version, status, size, message)
	return err
}
func (s *Service) listOne(r Region) (Region, error) {
	var size sql.NullInt64
	err := s.db.QueryRow(`SELECT status,size_bytes,error FROM map_packages WHERE id=?`, r.ID).Scan(&r.Status, &size, &r.Error)
	if errors.Is(err, sql.ErrNoRows) {
		r.Status = "available"
		return r, nil
	}
	if err != nil {
		return r, err
	}
	if size.Valid {
		r.SizeBytes = &size.Int64
	}
	return r, nil
}
func (s *Service) List() ([]Region, error) {
	result := make([]Region, 0, len(catalog))
	for _, r := range catalog {
		r, err := s.listOne(r)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, nil
}
func (s *Service) Start(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := find(id)
	if err != nil {
		return err
	}
	if s.closed || s.done != nil {
		return ErrState
	}
	current, err := s.listOne(r)
	if err != nil {
		return err
	}
	if current.Status == "ready" {
		return ErrState
	}
	if err := s.set(r, "queued", nil, ""); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	done := s.done
	go s.work(ctx, r, done)
	return nil
}
func (s *Service) Cancel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := find(id); err != nil {
		return err
	}
	if s.done == nil {
		return ErrState
	}
	s.cancel()
	return nil
}
func (s *Service) Close() error {
	s.mu.Lock()
	s.closed = true
	done := s.done
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	if done != nil {
		<-done
	}
	return nil
}
func (s *Service) binaryPath() (string, error) {
	if s.binary != "" {
		return s.binary, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, e := filepath.EvalSymlinks(exe); e == nil {
		exe = resolved
	}
	name := "pmtiles"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(filepath.Dir(exe), "runtime", "pmtiles", name), nil
}
func (s *Service) work(ctx context.Context, r Region, done chan struct{}) {
	defer func() { s.mu.Lock(); s.cancel = nil; s.done = nil; close(done); s.mu.Unlock() }()
	fail := func(msg string) { _ = os.Remove(s.part(r.ID)); _ = s.set(r, "error", nil, msg) }
	if err := s.set(r, "preparing", nil, ""); err != nil {
		fail("Harita durumu kaydedilemedi.")
		return
	}
	info, err := CheckSource(ctx, s.client, s.source)
	if err != nil || info.MinZoom != 0 || info.MaxZoom < 15 || info.TileType != 1 {
		fail("Harita kaynağına erişilemedi veya kaynak uyumsuz. Tekrar deneyin.")
		return
	}
	binary, err := s.binaryPath()
	if err != nil {
		fail("Harita çıkarma aracı bulunamadı.")
		return
	}
	if err := s.set(r, "downloading", nil, ""); err != nil {
		fail("Harita durumu kaydedilemedi.")
		return
	}
	bbox := fmt.Sprintf("%.7f,%.7f,%.7f,%.7f", r.Bounds[0], r.Bounds[1], r.Bounds[2], r.Bounds[3])
	if err := s.run(ctx, binary, "extract", s.source, s.part(r.ID), "--bbox="+bbox, "--maxzoom=15"); err != nil {
		if ctx.Err() != nil {
			fail("İndirme iptal edildi.")
		} else {
			fail("Harita indirilip çıkarılamadı. Tekrar deneyin.")
		}
		return
	}
	if err := s.set(r, "verifying", nil, ""); err != nil {
		fail("Harita durumu kaydedilemedi.")
		return
	}
	if err := s.run(ctx, binary, "verify", s.part(r.ID)); err != nil {
		fail("Harita doğrulanamadı. Tekrar deneyin.")
		return
	}
	if err := ctx.Err(); err != nil {
		fail("İndirme iptal edildi.")
		return
	}
	f, err := os.Open(s.part(r.ID))
	if err != nil {
		fail("Harita dosyası okunamadı.")
		return
	}
	h := make([]byte, headerSize)
	n, readErr := io.ReadFull(f, h)
	fi, statErr := f.Stat()
	f.Close()
	if readErr != nil || n != headerSize || statErr != nil || !fi.Mode().IsRegular() || string(h[:7]) != "PMTiles" || h[7] != 3 || h[99] != 1 || h[100] != 0 || h[101] != 15 {
		fail("Harita dosyası uyumsuz.")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		fail("İndirme iptal edildi.")
		return
	}
	if _, err := os.Lstat(s.path(r.ID)); !errors.Is(err, os.ErrNotExist) {
		fail("Harita hedef dosyası zaten var.")
		return
	}
	if err := os.Rename(s.part(r.ID), s.path(r.ID)); err != nil {
		fail("Harita kurulamadı.")
		return
	}
	size := fi.Size()
	if err := s.set(r, "ready", &size, ""); err != nil {
		_ = os.Remove(s.path(r.ID))
		fail("Harita kaydı oluşturulamadı.")
	}
}
func (s *Service) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := find(id)
	if err != nil {
		return err
	}
	if s.done != nil {
		return ErrState
	}
	item, err := s.listOne(r)
	if err != nil {
		return err
	}
	if item.Status != "ready" && item.Status != "error" {
		return ErrState
	}
	// Stage the removal so startup can restore it if metadata deletion fails
	// or the process is interrupted before the database commit.
	path, trash := s.path(id), s.path(id)+".deleting"
	staged := false
	if err := os.Rename(path, trash); err == nil {
		staged = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err = s.db.Exec(`DELETE FROM map_packages WHERE id=?`, id); err != nil {
		if staged {
			if restoreErr := os.Rename(trash, path); restoreErr != nil {
				return errors.Join(err, restoreErr)
			}
		}
		return err
	}
	if staged {
		if err := os.Remove(trash); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
func (s *Service) Open(id string) (*os.File, os.FileInfo, error) {
	r, err := find(id)
	if err != nil {
		return nil, nil, err
	}
	item, err := s.listOne(r)
	if err != nil {
		return nil, nil, err
	}
	if item.Status != "ready" {
		return nil, nil, ErrNotFound
	}
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	name := id + ".pmtiles"
	before, err := root.Lstat(name)
	if err != nil || !before.Mode().IsRegular() {
		return nil, nil, ErrNotFound
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, nil, err
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() || !os.SameFile(before, fi) {
		f.Close()
		return nil, nil, ErrNotFound
	}
	return f, fi, nil
}
