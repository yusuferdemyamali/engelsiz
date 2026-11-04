package translation_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"engelsiz/internal/database"
	"engelsiz/internal/download"
	"engelsiz/internal/translation"
)

type ze struct{ name, body string }

func buildZip(t *testing.T, entries []ze) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Store}
		if strings.HasSuffix(e.name, "/") {
			h.SetMode(os.ModeDir | 0o755)
			if _, err := w.CreateHeader(h); err != nil {
				t.Fatal(err)
			}
			continue
		}
		h.SetMode(0o644)
		fw, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(fw, e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func shaOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func goodEntries(from, to string) []ze {
	return goodEntriesVersion(from, to, "9.9.9-test")
}

// goodEntriesVersion mirrors the real minimal .argosmodel layout: a single
// top-level directory holding metadata.json, sentencepiece.model and
// model/model.bin plus a vocabulary payload.
func goodEntriesVersion(from, to, version string) []ze {
	return []ze{
		{name: "pkg/"},
		{name: "pkg/metadata.json", body: `{"from_code":"` + from + `","to_code":"` + to + `","package_version":"` + version + `"}`},
		{name: "pkg/sentencepiece.model", body: "fake-sentencepiece"},
		{name: "pkg/model/"},
		{name: "pkg/model/model.bin", body: "fake-model-payload"},
		{name: "pkg/model/shared_vocabulary.txt", body: "fake-vocab"},
	}
}

type fixture struct {
	db     *sql.DB
	dir    string
	binary string
	mgr    *download.Manager
	server *httptest.Server
	svc    *translation.Service
	pins   map[string]translation.Acquisition
	data   map[string][]byte
}

// reopenWithPins closes the fixture service and opens a new one on the same
// database and data root with one model's pins replaced.
func reopenWithPins(t *testing.T, fx *fixture, changed translation.Acquisition) (*translation.Service, error) {
	t.Helper()
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	pins := map[string]translation.Acquisition{}
	for id, p := range fx.pins {
		pins[id] = p
	}
	pins[translation.ModelTREN] = changed
	svc, err := translation.New(fx.db, fx.dir, translation.Options{Binary: fx.binary, Pins: pins})
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { svc.Close() })
	return svc, nil
}

func setupWithArchives(t *testing.T, opts translation.Options, archives map[string][]byte) *fixture {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := download.New(db, filepath.Join(dir, "downloads"), download.Options{})
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	fx := &fixture{db: db, dir: dir, binary: opts.Binary, mgr: mgr, data: map[string][]byte{}}
	generated := map[string][]byte{
		translation.ModelTREN: buildZip(t, goodEntries("tr", "en")),
		translation.ModelENTR: buildZip(t, goodEntries("en", "tr")),
	}
	for id, zb := range generated {
		if custom, ok := archives[id]; ok {
			zb = custom
		}
		fx.data[id] = zb
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, ok := fx.data[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	})
	fx.server = httptest.NewServer(mux)
	pins := map[string]translation.Acquisition{}
	// Test archives always have content, but pins are only auto-generated
	// when the caller passes no pin map at all. An explicitly empty map
	// exercises the unverified-metadata path.
	if opts.Pins == nil {
		for id, zb := range fx.data {
			pins[id] = translation.Acquisition{
				Title: "Test", Version: "9.9.9-test", FileName: id + ".zip",
				SourceURL:  fx.server.URL + "/" + id,
				TotalBytes: int64(len(zb)), ExpectedSize: int64(len(zb)),
				ChecksumValue: shaOf(zb),
			}
		}
	}
	for id, pin := range opts.Pins {
		pins[id] = pin
	}
	opts.Pins = pins
	svc, err := translation.New(db, dir, opts)
	if err != nil {
		fx.server.Close()
		mgr.Close()
		db.Close()
		t.Fatal(err)
	}
	fx.svc = svc
	fx.pins = pins
	t.Cleanup(func() { svc.Close(); fx.server.Close(); mgr.Close(); db.Close() })
	return fx
}

func setup(t *testing.T, opts translation.Options) *fixture {
	t.Helper()
	return setupWithArchives(t, opts, nil)
}

func isCode(err error, code string) bool {
	var problem *download.Error
	return errors.As(err, &problem) && problem.Code == code
}

func waitDownload(t *testing.T, m *download.Manager, id string) download.Snapshot {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		snap, err := m.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if snap.Status == "completed" {
			return snap
		}
		if snap.Status == "failed" || snap.Status == "cancelled" {
			t.Fatalf("download ended %s %+v", snap.Status, snap.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("download did not complete")
	return download.Snapshot{}
}

func waitModel(t *testing.T, s *translation.Service, m *download.Manager, id, want string) translation.Model {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		st, err := s.List(m)
		if err != nil {
			t.Fatal(err)
		}
		for _, model := range st.Models {
			if model.ID == id {
				if model.Status == want {
					return model
				}
				if model.Status == "error" && want != "error" {
					t.Fatalf("model %s error: %+v", id, model.Error)
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("model %s did not become %s", id, want)
	return translation.Model{}
}

func installFlow(t *testing.T, fx *fixture, id string) {
	t.Helper()
	snap, err := fx.svc.StartDownload(fx.mgr, id)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Provider != "argos" || snap.ProviderPackageID != id {
		t.Fatalf("unexpected snapshot %+v", snap)
	}
	waitDownload(t, fx.mgr, snap.ID)
	model, installed, err := fx.svc.Install(fx.mgr, id, snap.ID)
	if err != nil || installed {
		t.Fatalf("install accept: %+v %v %v", model, installed, err)
	}
	if model.Status != "installing" {
		t.Fatalf("accepted install must report installing, got %s", model.Status)
	}
	waitModel(t, fx.svc, fx.mgr, id, "ready")
	assertConsumed(t, fx, snap.ID)
}

func TestRoundTrip(t *testing.T) {
	fx := setup(t, translation.Options{Binary: fakeWorker(t)})
	installFlow(t, fx, translation.ModelTREN)
	st, err := fx.svc.List(fx.mgr)
	if err != nil {
		t.Fatal(err)
	}
	if !st.RuntimeAvailable || st.MaxCharacters != 5000 || len(st.Models) != 2 {
		t.Fatalf("status %+v", st)
	}
	got, err := fx.svc.Translate(context.Background(), "tr", "en", "merhaba dünya")
	if err != nil || got != "ÇEVİRİ:merhaba dünya" {
		t.Fatalf("translate %q %v", got, err)
	}
	got, err = fx.svc.Translate(context.Background(), "tr", "en", "satır bir\nsatır iki ğüşöçıİ")
	if err != nil || got != "ÇEVİRİ:satır bir\nsatır iki ğüşöçıİ" {
		t.Fatalf("multiline %q %v", got, err)
	}
}

func TestOneDirectionIndependent(t *testing.T) {
	fx := setup(t, translation.Options{Binary: fakeWorker(t)})
	installFlow(t, fx, translation.ModelTREN)
	if _, err := fx.svc.Translate(context.Background(), "tr", "en", "merhaba"); err != nil {
		t.Fatalf("installed direction failed: %v", err)
	}
	if _, err := fx.svc.Translate(context.Background(), "en", "tr", "hello"); !isCode(err, "invalid_state") {
		t.Fatalf("missing direction must be invalid_state, got %v", err)
	}
	items, err := fx.mgr.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ProviderPackageID == translation.ModelENTR {
			t.Fatal("en-tr download started without user action")
		}
	}
}

func TestCorruptHashRejected(t *testing.T) {
	fx := setup(t, translation.Options{Binary: fakeWorker(t)})
	snap, err := fx.svc.StartDownload(fx.mgr, translation.ModelTREN)
	if err != nil {
		t.Fatal(err)
	}
	waitDownload(t, fx.mgr, snap.ID)
	// Tamper the artifact on disk after completion: same length, wrong bytes.
	if err := os.WriteFile(downloadPartPath(t, fx, snap.ID), []byte(strings.Repeat("X", len(fx.data[translation.ModelTREN]))), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fx.svc.Install(fx.mgr, translation.ModelTREN, snap.ID); err != nil {
		t.Fatalf("install must be accepted async, got %v", err)
	}
	got := waitModel(t, fx.svc, fx.mgr, translation.ModelTREN, "error")
	if got.Error == nil || got.Error.Code != "verification_failed" {
		t.Fatalf("want verification_failed, got %+v", got.Error)
	}
	if _, err := os.Lstat(filepath.Join(fx.dir, "content", "translation", translation.ModelTREN)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupt model published: %v", err)
	}
}

func downloadPartPath(t *testing.T, fx *fixture, id string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(fx.dir, "downloads", id+".part"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("part file lookup: %v %v", matches, err)
	}
	return matches[0]
}

func TestCorruptSizeRejected(t *testing.T) {
	fx := setup(t, translation.Options{Binary: fakeWorker(t)})
	bad := fx.pins[translation.ModelTREN]
	bad.ExpectedSize++
	svc, err := reopenWithPins(t, fx, bad)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := svc.StartDownload(fx.mgr, translation.ModelTREN)
	if err != nil {
		t.Fatal(err)
	}
	waitDownload(t, fx.mgr, snap.ID)
	_, _, err = svc.Install(fx.mgr, translation.ModelTREN, snap.ID)
	if !isCode(err, "verification_failed") {
		t.Fatalf("size mismatch must fail fast, got %v", err)
	}
}

func TestZipSlipRejected(t *testing.T) {
	evil := buildZip(t, []ze{
		{name: "pkg/metadata.json", body: `{"from_code":"tr","to_code":"en"}`},
		{name: "pkg/model.bin", body: "x"},
		{name: "../evil.txt", body: "escape"},
	})
	fx := setupWithArchives(t, translation.Options{Binary: fakeWorker(t)},
		map[string][]byte{translation.ModelTREN: evil})
	snap, err := fx.svc.StartDownload(fx.mgr, translation.ModelTREN)
	if err != nil {
		t.Fatal(err)
	}
	waitDownload(t, fx.mgr, snap.ID)
	if _, _, err := fx.svc.Install(fx.mgr, translation.ModelTREN, snap.ID); err != nil {
		t.Fatalf("install must be accepted async, got %v", err)
	}
	got := waitModel(t, fx.svc, fx.mgr, translation.ModelTREN, "error")
	if got.Error == nil || got.Error.Code != "verification_failed" {
		t.Fatalf("want verification_failed, got %+v", got.Error)
	}
	for _, p := range []string{
		filepath.Join(fx.dir, "evil.txt"),
		filepath.Join(fx.dir, "content", "evil.txt"),
		filepath.Join(fx.dir, "content", "translation", translation.ModelTREN),
	} {
		if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unsafe path exists: %s", p)
		}
	}
	leftovers, _ := filepath.Glob(filepath.Join(fx.dir, "content", "translation", ".staging-*"))
	if len(leftovers) != 0 {
		t.Fatalf("staging leftovers: %v", leftovers)
	}
}

func TestMemberRejections(t *testing.T) {
	t.Helper()
	cases := map[string][]ze{
		"backslash":      {{name: `pkg\evil`, body: "x"}},
		"drive":          {{name: "C:/evil", body: "x"}},
		"absolute":       {{name: "/evil", body: "x"}},
		"dotdot":         {{name: "pkg/../../evil", body: "x"}},
		"empty":          {{name: "pkg/metadata.json", body: `{}`}},
		"case-collision": {{name: "pkg/A.bin", body: "x"}, {name: "pkg/a.bin", body: "y"}},
		"incompatible":   {{name: "meta/metadata.json", body: `{"from_code":"en","to_code":"tr"}`}, {name: "meta/model.bin", body: "x"}},
	}
	// Valid archives poisoned with a single bad member must still be rejected.
	poisoned := map[string]ze{
		"raw-traversal":  {name: "a/../evil.bin", body: "x"},
		"reserved":       {name: "pkg/CON", body: "x"},
		"trailing-dot":   {name: "pkg/evil.", body: "x"},
		"colon":          {name: "pkg/a:b", body: "x"},
		"trailing-space": {name: "pkg/evil ", body: "x"},
	}
	for name, bad := range poisoned {
		entries := append(append([]ze{}, goodEntries("tr", "en")...), bad)
		cases["poisoned-"+name] = entries
	}
	// Structurally valid archives with wrong content must fail verification.
	cases["missing-required"] = []ze{
		{name: "pkg/"},
		{name: "pkg/metadata.json", body: `{"from_code":"tr","to_code":"en","package_version":"9.9.9-test"}`},
		{name: "pkg/model/"},
		{name: "pkg/model/model.bin", body: "x"},
	}
	cases["wrong-version"] = goodEntriesVersion("tr", "en", "0.0.0")
	for name, entries := range cases {
		data := buildZip(t, entries)
		fx := setupWithArchives(t, translation.Options{Binary: fakeWorker(t)},
			map[string][]byte{translation.ModelTREN: data})
		snap, err := fx.svc.StartDownload(fx.mgr, translation.ModelTREN)
		if err != nil {
			t.Fatalf("%s: download: %v", name, err)
		}
		waitDownload(t, fx.mgr, snap.ID)
		if _, _, err := fx.svc.Install(fx.mgr, translation.ModelTREN, snap.ID); err != nil {
			t.Fatalf("%s: install accept: %v", name, err)
		}
		got := waitModel(t, fx.svc, fx.mgr, translation.ModelTREN, "error")
		if got.Error == nil {
			t.Fatalf("%s: expected error status", name)
		}
	}
}

func TestSymlinkMemberRejected(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: "pkg/link", Method: zip.Store}
	h.SetMode(os.ModeSymlink | 0o777)
	fw, err := w.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(fw, "target")
	mh := &zip.FileHeader{Name: "pkg/metadata.json", Method: zip.Store}
	mh.SetMode(0o644)
	mf, err := w.CreateHeader(mh)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(mf, `{"from_code":"tr","to_code":"en"}`)
	_ = w.Close()
	fx := setupWithArchives(t, translation.Options{Binary: fakeWorker(t)},
		map[string][]byte{translation.ModelTREN: buf.Bytes()})
	snap, err := fx.svc.StartDownload(fx.mgr, translation.ModelTREN)
	if err != nil {
		t.Fatal(err)
	}
	waitDownload(t, fx.mgr, snap.ID)
	if _, _, err := fx.svc.Install(fx.mgr, translation.ModelTREN, snap.ID); err != nil {
		t.Fatalf("install accept: %v", err)
	}
	waitModel(t, fx.svc, fx.mgr, translation.ModelTREN, "error")
}

func assertConsumed(t *testing.T, fx *fixture, downloadID string) {
	t.Helper()
	var state string
	if err := fx.db.QueryRow(`SELECT COALESCE(artifact_state,'') FROM downloads WHERE id=?`, downloadID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "consumed" {
		t.Fatalf("download %s artifact_state=%q, want consumed", downloadID, state)
	}
}

func TestFailedInstallNewAcquisitionThenRestart(t *testing.T) {
	binary := fakeWorker(t)
	fx := setup(t, translation.Options{Binary: binary})
	// First acquisition is tampered after download: install fails.
	snap1, err := fx.svc.StartDownload(fx.mgr, translation.ModelTREN)
	if err != nil {
		t.Fatal(err)
	}
	waitDownload(t, fx.mgr, snap1.ID)
	if err := os.WriteFile(downloadPartPath(t, fx, snap1.ID), []byte(strings.Repeat("X", len(fx.data[translation.ModelTREN]))), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fx.svc.Install(fx.mgr, translation.ModelTREN, snap1.ID); err != nil {
		t.Fatalf("install must be accepted async, got %v", err)
	}
	waitModel(t, fx.svc, fx.mgr, translation.ModelTREN, "error")
	// A new explicit acquisition supersedes the failed one and must be the
	// association GET shows (no stale download id).
	snap2, err := fx.svc.StartDownload(fx.mgr, translation.ModelTREN)
	if err != nil {
		t.Fatal(err)
	}
	if snap2.ID == snap1.ID {
		t.Fatal("new acquisition must have a new id")
	}
	st, err := fx.svc.List(fx.mgr)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range st.Models {
		if m.ID == translation.ModelTREN {
			if m.Download == nil || m.Download.ID != snap2.ID {
				t.Fatalf("GET shows stale download: %+v", m.Download)
			}
		}
	}
	waitDownload(t, fx.mgr, snap2.ID)
	if _, _, err := fx.svc.Install(fx.mgr, translation.ModelTREN, snap2.ID); err != nil {
		t.Fatal(err)
	}
	waitModel(t, fx.svc, fx.mgr, translation.ModelTREN, "ready")
	assertConsumed(t, fx, snap2.ID)
	// Restart without networking keeps the recovered model.
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	fx.server.Close()
	fx.mgr.Close()
	svc, err := translation.New(fx.db, fx.dir, translation.Options{Binary: binary, Pins: fx.pins})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	waitModel(t, svc, nil, translation.ModelTREN, "ready")
	got, err := svc.Translate(context.Background(), "tr", "en", "kurtarma sonrası")
	if err != nil || got != "ÇEVİRİ:kurtarma sonrası" {
		t.Fatalf("post-restart translate %q %v", got, err)
	}
}

func TestCorruptionIsolatedAndRepairable(t *testing.T) {
	binary := fakeWorker(t)
	fx := setup(t, translation.Options{Binary: binary})
	installFlow(t, fx, translation.ModelTREN)
	installFlow(t, fx, translation.ModelENTR)
	// Unknown user files beside our models must survive everything below.
	keep := filepath.Join(fx.dir, "content", "translation", "user-keep.txt")
	if err := os.WriteFile(keep, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Corrupt one payload behind the registry's back.
	modelBin := filepath.Join(fx.dir, "content", "translation", translation.ModelTREN, "model", "model.bin")
	fh, err := os.OpenFile(modelBin, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fh.Write([]byte("tamper")); err != nil {
		t.Fatal(err)
	}
	fh.Close()
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	svc, err := translation.New(fx.db, fx.dir, translation.Options{Binary: binary, Pins: fx.pins})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	st, err := svc.List(fx.mgr)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]translation.Model{}
	for _, m := range st.Models {
		byID[m.ID] = m
	}
	if byID[translation.ModelTREN].Status != "error" {
		t.Fatalf("corrupt model must surface error: %+v", byID[translation.ModelTREN])
	}
	if byID[translation.ModelENTR].Status != "ready" {
		t.Fatalf("healthy direction disabled by sibling corruption: %+v", byID[translation.ModelENTR])
	}
	if _, err := svc.Translate(context.Background(), "en", "tr", "hello"); err != nil {
		t.Fatalf("healthy direction must keep working: %v", err)
	}
	if _, err := svc.Translate(context.Background(), "tr", "en", "merhaba"); !isCode(err, "invalid_state") {
		t.Fatalf("corrupt model must refuse translation, got %v", err)
	}
	// Explicit repair: new acquisition reinstalls; broken dir staged aside.
	snap, err := svc.StartDownload(fx.mgr, translation.ModelTREN)
	if err != nil {
		t.Fatal(err)
	}
	waitDownload(t, fx.mgr, snap.ID)
	if _, _, err := svc.Install(fx.mgr, translation.ModelTREN, snap.ID); err != nil {
		t.Fatal(err)
	}
	waitModel(t, svc, fx.mgr, translation.ModelTREN, "ready")
	broken, _ := filepath.Glob(filepath.Join(fx.dir, "content", "translation", translation.ModelTREN+".broken-*"))
	if len(broken) != 1 {
		t.Fatalf("corrupt dir must be staged aside, got %v", broken)
	}
	if raw, err := os.ReadFile(keep); err != nil || string(raw) != "keep me" {
		t.Fatalf("user file disturbed: %q %v", raw, err)
	}
	got, err := svc.Translate(context.Background(), "tr", "en", "onarıldı")
	if err != nil || got != "ÇEVİRİ:onarıldı" {
		t.Fatalf("repaired translate %q %v", got, err)
	}
}

func TestQueueWaitBoundedByShutdown(t *testing.T) {
	binary := fakeWorker(t)
	fx := setup(t, translation.Options{Binary: binary})
	installFlow(t, fx, translation.ModelTREN)
	// Occupy the single global gate with a slow request.
	slowDone := make(chan error, 1)
	go func() {
		_, err := fx.svc.Translate(context.Background(), "tr", "en", "SLOW")
		slowDone <- err
	}()
	time.Sleep(500 * time.Millisecond)
	// A second request waits on the gate; Close must release it promptly.
	waitDone := make(chan error, 1)
	go func() {
		_, err := fx.svc.Translate(context.Background(), "tr", "en", "merhaba")
		waitDone <- err
	}()
	time.Sleep(500 * time.Millisecond)
	start := time.Now()
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-waitDone:
		if !isCode(err, "storage_error") {
			t.Fatalf("queued wait must fail with storage_error, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown did not bound the queued wait")
	}
	select {
	case <-slowDone:
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown did not interrupt in-flight work")
	}
	t.Logf("shutdown joined in %v", time.Since(start))
}

func TestRestartReusesModelWithoutNetwork(t *testing.T) {
	binary := fakeWorker(t)
	fx := setup(t, translation.Options{Binary: binary})
	installFlow(t, fx, translation.ModelTREN)
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	// Networking is gone; restart must recover from disk alone.
	fx.server.Close()
	fx.mgr.Close()
	svc, err := translation.New(fx.db, fx.dir, translation.Options{Binary: binary, Pins: fx.pins})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	st, err := svc.List(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range st.Models {
		if m.ID == translation.ModelTREN && m.Status != "ready" {
			t.Fatalf("restart lost model: %+v", m)
		}
	}
	got, err := svc.Translate(context.Background(), "tr", "en", "tekrar merhaba")
	if err != nil || got != "ÇEVİRİ:tekrar merhaba" {
		t.Fatalf("post-restart translate %q %v", got, err)
	}
}

func TestCrashRecovery(t *testing.T) {
	binary := fakeWorker(t)
	fx := setup(t, translation.Options{Binary: binary})
	installFlow(t, fx, translation.ModelTREN)
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash between publish and commit: orphan the final dir.
	if _, err := fx.db.Exec(`DELETE FROM translation_models WHERE id=?`, translation.ModelTREN); err != nil {
		t.Fatal(err)
	}
	// Simulate an interrupted install: installing row plus staging leftover.
	if _, err := fx.db.Exec(`INSERT INTO translation_models(id,version,status) VALUES(?,?,?)`, translation.ModelENTR, "9.9.9-test", "installing"); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(fx.dir, "content", "translation", ".staging-"+translation.ModelENTR+"-xyz")
	if err := os.MkdirAll(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	svc, err := translation.New(fx.db, fx.dir, translation.Options{Binary: binary, Pins: fx.pins})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	st, err := svc.List(fx.mgr)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]translation.Model{}
	for _, m := range st.Models {
		byID[m.ID] = m
	}
	if byID[translation.ModelTREN].Status != "ready" {
		t.Fatalf("orphan dir not adopted: %+v", byID[translation.ModelTREN])
	}
	if byID[translation.ModelENTR].Status != "error" {
		t.Fatalf("interrupted install not surfaced: %+v", byID[translation.ModelENTR])
	}
	if _, err := os.Lstat(staging); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging leftover not cleaned: %v", err)
	}
}

func TestTranslateValidation(t *testing.T) {
	fx := setup(t, translation.Options{Binary: "/nonexistent-worker"})
	ctx := context.Background()
	for name, tc := range map[string]struct {
		src, tgt, text string
		code           string
	}{
		"bad-pair":   {"tr", "tr", "merhaba", "invalid_request"},
		"third-lang": {"tr", "de", "merhaba", "invalid_request"},
		"empty":      {"tr", "en", "", "invalid_request"},
		"spaces":     {"tr", "en", "  \n\t ", "invalid_request"},
		"oversize":   {"tr", "en", strings.Repeat("a", 5001), "invalid_request"},
		"bad-utf8":   {"tr", "en", "ok\xff", "invalid_request"},
		"missing":    {"tr", "en", "merhaba", "invalid_state"},
	} {
		_, err := fx.svc.Translate(ctx, tc.src, tc.tgt, tc.text)
		if !isCode(err, tc.code) {
			t.Fatalf("%s: want %s got %v", name, tc.code, err)
		}
	}
	exact := strings.Repeat("ü", 5000)
	installFlow(t, fx, translation.ModelTREN)
	// Exact limit is accepted by validation; the missing-binary worker fails it.
	if _, err := fx.svc.Translate(ctx, "tr", "en", exact); !isCode(err, "engine_unavailable") {
		t.Fatalf("boundary: want engine_unavailable got %v", err)
	}
}

func TestMissingBinaryNoCrash(t *testing.T) {
	fx := setup(t, translation.Options{Binary: "/nonexistent-worker"})
	installFlow(t, fx, translation.ModelTREN)
	st, err := fx.svc.List(fx.mgr)
	if err != nil {
		t.Fatal(err)
	}
	if st.RuntimeAvailable {
		t.Fatal("runtime must be unavailable without binary")
	}
	if _, err := fx.svc.Translate(context.Background(), "tr", "en", "merhaba"); !isCode(err, "engine_unavailable") {
		t.Fatalf("want engine_unavailable, got %v", err)
	}
}

func TestTranslateTimeoutRestartsWorker(t *testing.T) {
	fx := setup(t, translation.Options{Binary: fakeWorker(t), TranslateTimeout: 300 * time.Millisecond})
	installFlow(t, fx, translation.ModelTREN)
	start := time.Now()
	_, err := fx.svc.Translate(context.Background(), "tr", "en", "SLEEP")
	if !isCode(err, "engine_unavailable") || time.Since(start) > 10*time.Second {
		t.Fatalf("timeout: %v in %v", err, time.Since(start))
	}
	// The killed worker must be replaced transparently on next request.
	got, err := fx.svc.Translate(context.Background(), "tr", "en", "merhaba")
	if err != nil || got != "ÇEVİRİ:merhaba" {
		t.Fatalf("after timeout %q %v", got, err)
	}
}

func TestTranslateMalformedWorkerResponse(t *testing.T) {
	fx := setup(t, translation.Options{Binary: fakeWorker(t)})
	installFlow(t, fx, translation.ModelTREN)
	for _, text := range []string{"BADJSON", "DOUBLE", "WORKERERROR"} {
		if _, err := fx.svc.Translate(context.Background(), "tr", "en", text); !isCode(err, "engine_unavailable") {
			t.Fatalf("%s: want engine_unavailable got %v", text, err)
		}
	}
	got, err := fx.svc.Translate(context.Background(), "tr", "en", "iyileşti")
	if err != nil || got != "ÇEVİRİ:iyileşti" {
		t.Fatalf("worker did not recover %q %v", got, err)
	}
}

func TestTranslateConcurrency(t *testing.T) {
	fx := setup(t, translation.Options{Binary: fakeWorker(t)})
	installFlow(t, fx, translation.ModelTREN)
	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	got := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			text := "eşzamanlı-" + strconv.Itoa(i)
			out, err := fx.svc.Translate(context.Background(), "tr", "en", text)
			got[i], errs[i] = out, err
		}(i)
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		if errs[i] != nil || got[i] != "ÇEVİRİ:eşzamanlı-"+strconv.Itoa(i) {
			t.Fatalf("[%d] %q %v", i, got[i], errs[i])
		}
	}
}

func TestPrivacy(t *testing.T) {
	var logs bytes.Buffer
	fx := setup(t, translation.Options{Binary: fakeWorker(t), Logger: log.New(&logs, "", 0)})
	installFlow(t, fx, translation.ModelTREN)
	marker := "GİZLİ-MARKER-987654321"
	got, err := fx.svc.Translate(context.Background(), "tr", "en", "ECHO_STDERR:"+marker)
	if err != nil || !strings.Contains(got, marker) {
		t.Fatalf("translate %q %v", got, err)
	}
	if strings.Contains(logs.String(), marker) {
		t.Fatalf("source text leaked to logs: %q", logs.String())
	}
	if _, err := fx.svc.Translate(context.Background(), "tr", "de", marker); err == nil {
		t.Fatal("expected invalid pair error")
	} else if strings.Contains(err.Error(), marker) {
		t.Fatalf("source text leaked to error: %v", err)
	}
}

func TestCloseInterruptsInflight(t *testing.T) {
	binary := fakeWorker(t)
	fx := setup(t, translation.Options{Binary: binary})
	installFlow(t, fx, translation.ModelTREN)
	done := make(chan error, 1)
	go func() {
		_, err := fx.svc.Translate(context.Background(), "tr", "en", "SLOW")
		done <- err
	}()
	time.Sleep(500 * time.Millisecond)
	start := time.Now()
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("in-flight translate must fail after Close")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not interrupt in-flight work")
	}
	t.Logf("interrupted in %v", time.Since(start))
}

func TestNoReplaceReadyModel(t *testing.T) {
	fx := setup(t, translation.Options{Binary: fakeWorker(t)})
	installFlow(t, fx, translation.ModelTREN)
	// A ready model at the same pinned version is not re-downloaded.
	if _, err := fx.svc.StartDownload(fx.mgr, translation.ModelTREN); !isCode(err, "target_collision") {
		t.Fatalf("redownload of ready model must be refused, got %v", err)
	}
	// Idempotent retry with the original download stays ready.
	st, _ := fx.svc.List(fx.mgr)
	var ready translation.Model
	for _, m := range st.Models {
		if m.ID == translation.ModelTREN {
			ready = m
		}
	}
	if ready.Download == nil {
		t.Fatal("expected associated download")
	}
	model, installed, err := fx.svc.Install(fx.mgr, translation.ModelTREN, ready.Download.ID)
	if err != nil || !installed || model.Status != "ready" {
		t.Fatalf("idempotent reinstall: %+v %v %v", model, installed, err)
	}
}

func TestUnverifiedPinsDisabled(t *testing.T) {
	// An explicitly empty pin map exercises the unverified path.
	fx := setup(t, translation.Options{Pins: map[string]translation.Acquisition{}})
	if _, err := fx.svc.StartDownload(fx.mgr, translation.ModelTREN); !isCode(err, "metadata_unavailable") {
		t.Fatalf("unverified model must refuse downloads, got %v", err)
	}
	st, err := fx.svc.List(fx.mgr)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range st.Models {
		if m.Status != "missing" {
			t.Fatalf("unverified model must be missing: %+v", m)
		}
	}
}

func TestDefaultPinsVerified(t *testing.T) {
	t.Helper()
	for _, id := range []string{translation.ModelTREN, translation.ModelENTR} {
		pin, ok := translation.DefaultPins[id]
		if !ok {
			t.Fatalf("missing default pin %s", id)
		}
		if pin.Version == "" || pin.FileName == "" || pin.SourceURL == "" {
			t.Fatalf("pin %s has empty metadata: %+v", id, pin)
		}
		if pin.TotalBytes <= 0 || pin.ExpectedSize <= 0 || pin.TotalBytes != pin.ExpectedSize {
			t.Fatalf("pin %s has bad sizes: %+v", id, pin)
		}
		raw, err := hex.DecodeString(pin.ChecksumValue)
		if err != nil || len(raw) != 32 || pin.ChecksumValue != strings.ToLower(pin.ChecksumValue) {
			t.Fatalf("pin %s has bad sha256: %v", id, err)
		}
		u, err := url.Parse(pin.SourceURL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
			t.Fatalf("pin %s has unsafe URL %q: %v", id, pin.SourceURL, err)
		}
		if strings.ContainsAny(pin.FileName, `/\:*?"<>|`) || strings.HasSuffix(pin.FileName, ".") || strings.TrimSpace(pin.FileName) != pin.FileName {
			t.Fatalf("pin %s has unsafe filename %q", id, pin.FileName)
		}
	}
	// The catalog default must actually activate both directions.
	active := translation.Definitions(translation.DefaultPins)
	if len(active) != 2 || !active[0].Verified() || !active[1].Verified() {
		t.Fatalf("default catalog not verified: %+v", active)
	}
}

func TestSortedDeterminism(t *testing.T) {
	fx := setup(t, translation.Options{Binary: fakeWorker(t)})
	st, err := fx.svc.List(fx.mgr)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{st.Models[0].ID, st.Models[1].ID}
	want := []string{translation.ModelTREN, translation.ModelENTR}
	sort.Strings(ids)
	sorted := append([]string{}, want...)
	sort.Strings(sorted)
	if ids[0] != sorted[0] || ids[1] != sorted[1] {
		t.Fatalf("models %+v", ids)
	}
	if st.Models[0].ID != translation.ModelTREN || st.Models[1].ID != translation.ModelENTR {
		t.Fatalf("stable order want tr-en,en-tr got %s,%s", st.Models[0].ID, st.Models[1].ID)
	}
}
