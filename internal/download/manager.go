package download

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Options struct {
	Logger     *log.Logger
	HTTPClient *http.Client
	Available  func(string) (uint64, error)
	Write      func(*os.File, []byte) (int, error)
}
type worker struct {
	id     string
	cancel context.CancelFunc
	done   chan struct{}
}
type Manager struct {
	logger     *log.Logger
	db         *sql.DB
	dir        string
	root       *os.Root
	client     *http.Client
	available  func(string) (uint64, error)
	write      func(*os.File, []byte) (int, error)
	ops        sync.Mutex
	mu         sync.Mutex
	active     *worker
	closed     bool
	persistErr error
	wake       chan struct{}
	stop       chan struct{}
	finished   chan struct{}
}

func New(db *sql.DB, directory string, opts Options) (*Manager, error) {
	dir, e := filepath.Abs(directory)
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	// Reject the download directory itself being replaced by a symlink.
	info, e := os.Lstat(dir)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrStorage
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		return nil, e
	}
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ResponseHeaderTimeout: 30 * time.Second, TLSHandshakeTimeout: 15 * time.Second, IdleConnTimeout: 90 * time.Second, DisableCompression: true}
	client := &http.Client{Transport: transport}
	if opts.HTTPClient != nil {
		copy := *opts.HTTPClient
		copy.Timeout = 0
		client = &copy
	}
	redirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || (req.URL.Scheme != "http" && req.URL.Scheme != "https") || req.URL.User != nil || (via[0].URL.Scheme == "https" && req.URL.Scheme != "https") {
			return ErrSource
		}
		if redirect != nil {
			return redirect(req, via)
		}
		return nil
	}
	m := &Manager{logger: opts.Logger, db: db, dir: dir, root: root, client: client, available: opts.Available, write: opts.Write, wake: make(chan struct{}, 1), stop: make(chan struct{}), finished: make(chan struct{})}
	if m.available == nil {
		m.available = diskAvailable
	}
	if m.write == nil {
		m.write = func(f *os.File, b []byte) (int, error) { return f.Write(b) }
	}
	if e = m.recover(); e != nil {
		m.diagnostic("recovery", "", ErrStorage)
		root.Close()
		return nil, e
	}
	go m.loop()
	return m, nil
}
func (m *Manager) healthy() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.persistErr != nil {
		return ErrStorage
	}
	return nil
}
func (m *Manager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}
func (m *Manager) path(id string) string { return filepath.Join(m.dir, id+".part") }
func (m *Manager) owned(r record) bool {
	id, e := hex.DecodeString(r.ID)
	return e == nil && len(id) == 16 && r.TempPath == m.path(r.ID) && SafeFilename(r.FileName)
}
func (m *Manager) stat(r record) (int64, error) {
	if !m.owned(r) {
		return 0, ErrStorage
	}
	info, e := m.root.Lstat(r.ID + ".part")
	if errors.Is(e, os.ErrNotExist) {
		return 0, nil
	}
	if e != nil {
		return 0, e
	}
	if !info.Mode().IsRegular() {
		return 0, ErrStorage
	}
	return info.Size(), nil
}
func (m *Manager) recover() error {
	records, e := m.list()
	if e != nil {
		return e
	}
	for _, r := range records {
		if r.Status == "cancelled" || (r.Status == "completed" && r.ArtifactOwner != "" && (r.ArtifactState == "claimed" || r.ArtifactState == "consumed")) {
			continue
		}
		n, e := m.stat(r)
		if e != nil || (r.TotalBytes != nil && n > *r.TotalBytes) {
			r.Status = "failed"
			r.Error = ErrStorage
		} else {
			r.DownloadedBytes = n
			if r.Status == "queued" || r.Status == "downloading" {
				r.Status = "paused"
				r.Error = nil
			}
			if r.Status == "completed" && (r.TotalBytes == nil || n != *r.TotalBytes) {
				r.Status = "failed"
				r.Error = ErrStorage
			}
		}
		if e = m.save(&r); e != nil {
			return e
		}
		if r.Error != nil {
			m.diagnostic("recovery", r.ID, r.Error)
		}
	}
	return nil
}
func (m *Manager) checkSpace(total *int64, offset int64) error {
	free, e := m.available(m.dir)
	if e != nil || free == 0 {
		return ErrSpace
	}
	if total != nil {
		remaining := *total - offset
		if remaining < 0 || uint64(remaining) > free {
			return ErrSpace
		}
	}
	return nil
}
func (m *Manager) Create(d Descriptor) (snapshot Snapshot, result error) {
	defer func() {
		if result != nil {
			m.diagnostic("create", "", result)
		}
	}()
	m.ops.Lock()
	defer m.ops.Unlock()
	if e := m.healthy(); e != nil {
		return Snapshot{}, e
	}
	if !validDescriptor(d) {
		return Snapshot{}, ErrInvalid
	}
	var n int
	if e := m.db.QueryRow(`SELECT COUNT(*) FROM downloads WHERE provider=? AND provider_package_id=? AND status NOT IN ('cancelled','completed')`, d.Provider, d.ProviderPackageID).Scan(&n); e != nil {
		return Snapshot{}, ErrStorage
	}
	if n > 0 {
		return Snapshot{}, ErrExists
	}
	if e := m.checkSpace(d.TotalBytes, 0); e != nil {
		return Snapshot{}, e
	}
	id := make([]byte, 16)
	if _, e := rand.Read(id); e != nil {
		return Snapshot{}, ErrStorage
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	r := record{Snapshot: Snapshot{ID: hex.EncodeToString(id), Provider: d.Provider, ProviderPackageID: d.ProviderPackageID, Title: d.Title, Variant: d.Variant, FileName: d.FileName, Status: "queued", TotalBytes: d.TotalBytes, CreatedAt: now, UpdatedAt: now}, SourceURL: d.SourceURL, ContentType: d.ContentType, Language: d.Language, Version: d.Version, ExpectedSize: d.ExpectedSize, ChecksumAlgorithm: d.ChecksumAlgorithm, ChecksumValue: d.ChecksumValue}
	r.TempPath = m.path(r.ID)
	if e := m.insert(&r); e != nil {
		return Snapshot{}, ErrStorage
	}
	m.diagnostic("queued", r.ID, nil)
	m.signal()
	return r.snapshot(), nil
}
func (m *Manager) List() ([]Snapshot, error) {
	if e := m.healthy(); e != nil {
		return nil, e
	}
	records, e := m.list()
	if e != nil {
		return nil, ErrStorage
	}
	out := make([]Snapshot, 0, len(records))
	for _, r := range records {
		out = append(out, r.snapshot())
	}
	return out, nil
}
func (m *Manager) Get(id string) (Snapshot, error) {
	if e := m.healthy(); e != nil {
		return Snapshot{}, e
	}
	r, e := m.get(id)
	if e != nil {
		return Snapshot{}, publicError(e)
	}
	return r.snapshot(), nil
}
func (m *Manager) Control(id, action string) (snapshot Snapshot, result error) {
	defer func() {
		if result != nil {
			m.diagnostic("control", id, result)
		}
	}()
	m.ops.Lock()
	defer m.ops.Unlock()
	if e := m.healthy(); e != nil {
		return Snapshot{}, e
	}
	r, e := m.get(id)
	if e != nil {
		return Snapshot{}, publicError(e)
	}
	target := ""
	switch action {
	case "pause":
		if r.Status == "paused" {
			return r.snapshot(), nil
		}
		if r.Status == "queued" || r.Status == "downloading" {
			target = "paused"
		}
	case "resume":
		if r.Status == "queued" || r.Status == "downloading" {
			return r.snapshot(), nil
		}
		if r.Status == "paused" {
			target = "queued"
		}
	case "retry":
		if r.Status == "failed" {
			target = "queued"
		}
	case "cancel":
		if r.Status == "cancelled" {
			return r.snapshot(), nil
		}
		if r.Status != "completed" {
			target = "cancelled"
		}
	default:
		return Snapshot{}, ErrInvalid
	}
	if target == "" {
		return Snapshot{}, ErrState
	}
	m.mu.Lock()
	active := m.active
	if active != nil && active.id == id {
		active.cancel()
	} else {
		active = nil
	}
	m.mu.Unlock()
	if active != nil {
		<-active.done
		if e = m.healthy(); e != nil {
			return Snapshot{}, e
		}
		r, e = m.get(id)
		if e != nil {
			return Snapshot{}, publicError(e)
		}
		if r.Status == "completed" {
			return Snapshot{}, ErrState
		}
	}
	// Completion may race the initial state read. Once the worker has joined,
	// re-read its terminal state before applying a control.
	r, e = m.get(id)
	if e != nil {
		return Snapshot{}, publicError(e)
	}
	if r.Status == "completed" || r.Status == "cancelled" {
		return Snapshot{}, ErrState
	}
	if target == "cancelled" {
		if !m.owned(r) {
			return Snapshot{}, ErrStorage
		}
		if _, e = m.stat(r); e != nil {
			return Snapshot{}, ErrStorage
		}
		e = m.root.Remove(r.ID + ".part")
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return Snapshot{}, ErrStorage
		}
		r.DownloadedBytes = 0
	}
	r.Status = target
	r.Error = nil
	if e = m.save(&r); e != nil {
		return Snapshot{}, ErrStorage
	}
	if target == "queued" {
		m.signal()
	}
	return r.snapshot(), nil
}
func (m *Manager) loop() {
	defer close(m.finished)
	for {
		select {
		case <-m.stop:
			return
		case <-m.wake:
		}
		for {
			m.ops.Lock()
			m.mu.Lock()
			if m.closed || m.persistErr != nil {
				m.mu.Unlock()
				m.ops.Unlock()
				break
			}
			records, e := m.list()
			var next *record
			if e == nil {
				for i := len(records) - 1; i >= 0; i-- {
					if records[i].Status == "queued" {
						next = &records[i]
						break
					}
				}
			}
			if e != nil {
				m.persistErr = e
				m.diagnostic("queue", "", ErrStorage)
			}
			if next == nil {
				m.mu.Unlock()
				m.ops.Unlock()
				break
			}
			r := *next
			r.Status = "downloading"
			r.Error = nil
			if e = m.save(&r); e != nil {
				m.persistErr = e
				m.diagnostic("checkpoint", r.ID, ErrStorage)
				m.mu.Unlock()
				m.ops.Unlock()
				break
			}
			ctx, cancel := context.WithCancel(context.Background())
			w := &worker{r.ID, cancel, make(chan struct{})}
			m.active = w
			m.mu.Unlock()
			m.ops.Unlock()
			e = m.transfer(ctx, &r)
			cancel()
			if ctx.Err() != nil && errors.Is(e, context.Canceled) {
				r.Status = "paused"
				r.Error = nil
			} else if e != nil {
				r.Status = "failed"
				r.Error = publicError(e)
			} else {
				r.Status = "completed"
				r.Error = nil
			}
			m.mu.Lock()
			if saveErr := m.save(&r); saveErr != nil {
				m.persistErr = saveErr
				m.diagnostic("checkpoint", r.ID, ErrStorage)
			}
			m.diagnostic("transfer", r.ID, e)
			m.active = nil
			close(w.done)
			m.mu.Unlock()
		}
	}
}
func (m *Manager) Close() error {
	m.ops.Lock()
	m.mu.Lock()
	if m.closed {
		e := m.persistErr
		m.mu.Unlock()
		m.ops.Unlock()
		return e
	}
	m.closed = true
	active := m.active
	if active != nil {
		active.cancel()
	}
	close(m.stop)
	m.mu.Unlock()
	if active != nil {
		<-active.done
	}
	m.ops.Unlock()
	<-m.finished
	m.client.CloseIdleConnections()
	e := m.root.Close()
	m.mu.Lock()
	defer m.mu.Unlock()
	return errors.Join(e, m.persistErr)
}
func usableETag(s string) bool {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return false
	}
	for i := 1; i < len(s)-1; i++ {
		c := s[i]
		if c != 0x21 && (c < 0x23 || c == 0x7f) {
			return false
		}
	}
	return true
}
func validator(r *record) string {
	if usableETag(r.ETag) {
		return r.ETag
	}
	if _, e := http.ParseTime(r.LastModified); e == nil {
		return r.LastModified
	}
	return ""
}

// Diagnostics deliberately omit metadata, URLs and error messages. A custom
// HTTP client or writer may return errors containing credentials or body text.
func (m *Manager) diagnostic(operation, id string, err error) {
	if m.logger == nil {
		return
	}
	if decoded, e := hex.DecodeString(id); e != nil || len(decoded) != 16 {
		id = "unassigned"
	}
	code := "ok"
	if errors.Is(err, context.Canceled) {
		code = "interrupted"
	} else if err != nil {
		switch publicError(err).Code {
		case "not_found", "invalid_state", "download_exists", "invalid_request", "insufficient_space", "source_unavailable", "storage_error", "ownership_conflict":
			code = publicError(err).Code
		default:
			code = "storage_error"
		}
	}
	m.logger.Printf("download %s: id=%s code=%s", operation, id, code)
}
