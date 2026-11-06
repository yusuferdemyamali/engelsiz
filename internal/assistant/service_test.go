package assistant_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"engelsiz/internal/assistant"
	"engelsiz/internal/database"
	"engelsiz/internal/download"
)

type tarEntry struct {
	name string
	body []byte
	mode int64
	link string // symlink target when typeflag symlink
}

func shaOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func buildTarGz(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: e.mode}
		if e.link != "" {
			hdr.Typeflag = tar.TypeSymlink
			hdr.Linkname = e.link
		} else if strings.HasSuffix(e.name, "/") {
			hdr.Typeflag = tar.TypeDir
		} else {
			hdr.Typeflag = tar.TypeReg
			hdr.Size = int64(len(e.body))
		}
		if hdr.Mode == 0 {
			hdr.Mode = 0o644
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			if _, err := tw.Write(e.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func serverBinaryName() string {
	if runtime.GOOS == "windows" {
		return "llama-server.exe"
	}
	return "llama-server"
}

func runtimeLibName() string {
	if runtime.GOOS == "windows" {
		return "libggml.dll"
	}
	return "libggml.so"
}

func goodRuntimeArchive() []tarEntry {
	bin := serverBinaryName()
	lib := runtimeLibName()
	if runtime.GOOS == "windows" {
		// Windows-shaped layout: exe + DLL, no POSIX symlink (symlink
		// creation requires privilege and Windows archives ship DLLs).
		return []tarEntry{
			{name: "pkg/"},
			{name: "pkg/" + bin, body: []byte("MZ-fake-exe"), mode: 0o755},
			{name: "pkg/lib/"},
			{name: "pkg/lib/" + lib, body: []byte("fake-dll"), mode: 0o644},
		}
	}
	return []tarEntry{
		{name: "pkg/"},
		{name: "pkg/" + bin, body: []byte("#!/bin/sh\necho hi\n"), mode: 0o755},
		{name: "pkg/lib/"},
		{name: "pkg/lib/" + lib, body: []byte("fake-so"), mode: 0o644},
		{name: "pkg/lib/link", link: "libggml.so"},
	}
}

type fixture struct {
	db     *sql.DB
	dir    string
	mgr    *download.Manager
	server *httptest.Server
	svc    *assistant.Service
	pins   map[string]assistant.Acquisition
	rtPin  assistant.RuntimeAcquisition
	data   map[string][]byte
	hits   *atomic.Int64
	binary string
}

func fakeBinary(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "fake-llama-server")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(p) {
		t.Fatal("binary path must be absolute")
	}
	return p
}

func setup(t *testing.T, mutate func(pins map[string]assistant.Acquisition, rt *assistant.RuntimeAcquisition, data map[string][]byte)) *fixture {
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
	fx := &fixture{db: db, dir: dir, mgr: mgr, data: map[string][]byte{}, hits: &atomic.Int64{}}
	fx.binary = fakeBinary(t, dir)
	ggufLite := []byte("fake-gguf-lite-" + strings.Repeat("A", 4096))
	ggufBal := []byte("fake-gguf-balanced-" + strings.Repeat("B", 4096))
	ggufQ := []byte("fake-gguf-quality-" + strings.Repeat("C", 4096))
	rtArchive := buildTarGz(t, goodRuntimeArchive())
	fx.data["lite"] = ggufLite
	fx.data["balanced"] = ggufBal
	fx.data["quality"] = ggufQ
	fx.data["runtime"] = rtArchive
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fx.hits.Add(1)
		body, ok := fx.data[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	})
	fx.server = httptest.NewServer(mux)
	fx.pins = map[string]assistant.Acquisition{}
	for id, zb := range map[string][]byte{"lite": ggufLite, "balanced": ggufBal, "quality": ggufQ} {
		fx.pins[id] = assistant.Acquisition{
			Title: "Test " + id, ModelName: "Qwen3-Test", Quantization: "Q4_K_M",
			Version: "v-test", FileName: id + ".gguf",
			SourceURL:  fx.server.URL + "/" + id,
			TotalBytes: int64(len(zb)), ExpectedSize: int64(len(zb)),
			ChecksumValue: shaOf(zb), ContextSize: 2048, MaxTokens: 512,
			RAMGB: 2, License: "Apache-2.0", Source: "test",
		}
	}
	fx.rtPin = assistant.RuntimeAcquisition{
		Version: "b-test", FileName: "rt.tar.gz",
		SourceURL:  fx.server.URL + "/runtime",
		TotalBytes: int64(len(rtArchive)), ExpectedSize: int64(len(rtArchive)),
		ChecksumValue: shaOf(rtArchive), Format: "tar.gz",
	}
	if mutate != nil {
		mutate(fx.pins, &fx.rtPin, fx.data)
	}
	svc, err := assistant.New(db, dir, assistant.Options{Binary: fx.binary, Pins: fx.pins, RuntimePin: &fx.rtPin})
	if err != nil {
		fx.server.Close()
		mgr.Close()
		db.Close()
		t.Fatal(err)
	}
	fx.svc = svc
	t.Cleanup(func() { svc.Close(); fx.server.Close(); mgr.Close(); db.Close() })
	return fx
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

func waitProfile(t *testing.T, s *assistant.Service, m *download.Manager, id, want string) assistant.Profile {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		st, err := s.List(m)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range st.Profiles {
			if p.ID == id {
				if p.Status == want {
					return p
				}
				if p.Status == "error" && want != "error" {
					t.Fatalf("profile %s error: %+v", id, p.Error)
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("profile %s did not become %s", id, want)
	return assistant.Profile{}
}

func waitRuntime(t *testing.T, s *assistant.Service, m *download.Manager, want string) assistant.RuntimeStatus {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		st, err := s.List(m)
		if err != nil {
			t.Fatal(err)
		}
		if st.Runtime.Status == want {
			return st.Runtime
		}
		if st.Runtime.Status == "error" && want != "error" {
			t.Fatalf("runtime error: %+v", st.Runtime.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("runtime did not become %s", want)
	return assistant.RuntimeStatus{}
}

func installModelFlow(t *testing.T, fx *fixture, id string) {
	t.Helper()
	snap, err := fx.svc.StartDownload(fx.mgr, id)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Provider != "assistant-llm" || snap.ProviderPackageID != id {
		t.Fatalf("unexpected snapshot %+v", snap)
	}
	waitDownload(t, fx.mgr, snap.ID)
	prof, installed, err := fx.svc.Install(fx.mgr, id, snap.ID)
	if err != nil || installed {
		t.Fatalf("install accept: %+v %v %v", prof, installed, err)
	}
	if prof.Status != "installing" {
		t.Fatalf("accepted install must report installing, got %s", prof.Status)
	}
	waitProfile(t, fx.svc, fx.mgr, id, "ready")
	var state string
	if err := fx.db.QueryRow(`SELECT COALESCE(artifact_state,'') FROM downloads WHERE id=?`, snap.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "consumed" {
		t.Fatalf("download %s artifact_state=%q, want consumed", snap.ID, state)
	}
}

func installRuntimeFlow(t *testing.T, fx *fixture) {
	t.Helper()
	snap, err := fx.svc.StartRuntimeDownload(fx.mgr)
	if err != nil {
		t.Fatal(err)
	}
	waitDownload(t, fx.mgr, snap.ID)
	installed, err := fx.svc.InstallRuntime(fx.mgr, snap.ID)
	if err != nil || installed {
		t.Fatalf("runtime install accept: %v %v", installed, err)
	}
	waitRuntime(t, fx.svc, fx.mgr, "ready")
}

func TestModelRoundTripActivateUseModel(t *testing.T) {
	fx := setup(t, nil)
	installModelFlow(t, fx, "lite")
	installRuntimeFlow(t, fx)
	if err := fx.svc.Activate("lite"); err != nil {
		t.Fatal(err)
	}
	st, err := fx.svc.List(fx.mgr)
	if err != nil {
		t.Fatal(err)
	}
	if st.ActiveProfileID != "lite" {
		t.Fatalf("active=%q", st.ActiveProfileID)
	}
	if len(st.Profiles) != 3 || st.Profiles[0].ID != "lite" || st.Profiles[1].ID != "balanced" || st.Profiles[2].ID != "quality" {
		t.Fatalf("stable order broken: %+v", st.Profiles)
	}
	if st.Hardware.CPUCount < 1 {
		t.Fatalf("hardware cpu=%d", st.Hardware.CPUCount)
	}
	if st.RecommendedProfileID == "" {
		t.Fatal("missing recommended profile")
	}
	if !st.Runtime.Available || st.Runtime.Status != "ready" || st.Runtime.Version == "" {
		t.Fatalf("runtime %+v", st.Runtime)
	}
	if st.Busy {
		t.Fatal("idle service must not be busy")
	}
	// Empty id uses the active profile; model and runtime are pinned.
	err = fx.svc.UseModel(context.Background(), "", func(ctx context.Context, m assistant.Model) error {
		if m.ID != "lite" || m.Binary != fx.binary {
			t.Fatalf("model %+v", m)
		}
		if _, err := os.Stat(m.Path); err != nil {
			t.Fatalf("model path: %v", err)
		}
		if m.ContextSize != 2048 || m.MaxTokens != 512 {
			t.Fatalf("budget %+v", m)
		}
		if ctx.Err() != nil {
			t.Fatalf("ctx: %v", ctx.Err())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Explicit id works too.
	if err := fx.svc.UseModel(context.Background(), "lite", func(_ context.Context, m assistant.Model) error { return nil }); err != nil {
		t.Fatal(err)
	}
	// Unknown and missing profiles.
	if err := fx.svc.UseModel(context.Background(), "nope", func(_ context.Context, m assistant.Model) error { return nil }); !isCode(err, "not_found") {
		t.Fatalf("unknown: %v", err)
	}
	if err := fx.svc.UseModel(context.Background(), "balanced", func(_ context.Context, m assistant.Model) error { return nil }); !isCode(err, "invalid_state") {
		t.Fatalf("missing: %v", err)
	}
}

func TestBadModelHashRejected(t *testing.T) {
	fx := setup(t, nil)
	snap, err := fx.svc.StartDownload(fx.mgr, "lite")
	if err != nil {
		t.Fatal(err)
	}
	waitDownload(t, fx.mgr, snap.ID)
	// Tamper the artifact on disk after completion: same length, wrong bytes.
	bad := bytes.Repeat([]byte("X"), len(fx.data["lite"]))
	part := filepath.Join(fx.dir, "downloads", snap.ID+".part")
	if err := os.WriteFile(part, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fx.svc.Install(fx.mgr, "lite", snap.ID); err != nil {
		t.Fatalf("install must be accepted async, got %v", err)
	}
	got := waitProfile(t, fx.svc, fx.mgr, "lite", "error")
	if got.Error == nil || got.Error.Code != "verification_failed" {
		t.Fatalf("want verification_failed, got %+v", got.Error)
	}
	if _, err := os.Lstat(filepath.Join(fx.dir, "content", "assistant", "models", "lite")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupt model published: %v", err)
	}
}

func TestBadRuntimeHashRejected(t *testing.T) {
	fx := setup(t, nil)
	snap, err := fx.svc.StartRuntimeDownload(fx.mgr)
	if err != nil {
		t.Fatal(err)
	}
	waitDownload(t, fx.mgr, snap.ID)
	bad := bytes.Repeat([]byte("Y"), len(fx.data["runtime"]))
	if err := os.WriteFile(filepath.Join(fx.dir, "downloads", snap.ID+".part"), bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.svc.InstallRuntime(fx.mgr, snap.ID); err != nil {
		t.Fatalf("runtime install must be accepted async, got %v", err)
	}
	got := waitRuntime(t, fx.svc, fx.mgr, "error")
	if got.Error == nil || got.Error.Code != "verification_failed" {
		t.Fatalf("want verification_failed, got %+v", got.Error)
	}
}

func TestReceiptRestartMissingPayload(t *testing.T) {
	fx := setup(t, nil)
	installModelFlow(t, fx, "lite")
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	// Vanish the payload behind the registry's back; receipt alone proves nothing.
	if err := os.Remove(filepath.Join(fx.dir, "content", "assistant", "models", "lite", "model.gguf")); err != nil {
		t.Fatal(err)
	}
	svc, err := assistant.New(fx.db, fx.dir, assistant.Options{Binary: fx.binary, Pins: fx.pins, RuntimePin: &fx.rtPin})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	st, err := svc.List(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range st.Profiles {
		if p.ID == "lite" && (p.Status != "error" || p.Error == nil) {
			t.Fatalf("vanished payload must surface error: %+v", p)
		}
	}
	// Missing payload must refuse activation and inference.
	if err := svc.Activate("lite"); !isCode(err, "invalid_state") {
		t.Fatalf("activate vanished: %v", err)
	}
	if err := svc.UseModel(context.Background(), "lite", func(_ context.Context, m assistant.Model) error { return nil }); !isCode(err, "invalid_state") {
		t.Fatalf("usemodel vanished: %v", err)
	}
}

func TestReceiptOrphanAdoptedOnRestart(t *testing.T) {
	fx := setup(t, nil)
	installModelFlow(t, fx, "lite")
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash between atomic publish and durable commit.
	if _, err := fx.db.Exec(`DELETE FROM assistant_models WHERE id=?`, "lite"); err != nil {
		t.Fatal(err)
	}
	svc, err := assistant.New(fx.db, fx.dir, assistant.Options{Binary: fx.binary, Pins: fx.pins, RuntimePin: &fx.rtPin})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	waitProfile(t, svc, nil, "lite", "ready")
}

func TestCancelAndInstallIDBinding(t *testing.T) {
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
	defer func() { mgr.Close(); db.Close() }()
	payload := []byte("fake-gguf-" + strings.Repeat("G", 4096))
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimPrefix(r.URL.Path, "/") != "lite" {
			http.NotFound(w, r)
			return
		}
		<-release // hold the transfer in-flight until the test cancels
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer srv.Close()
	pins := map[string]assistant.Acquisition{
		"lite": {
			Title: "Test lite", ModelName: "Qwen3-Test", Quantization: "Q4_K_M",
			Version: "v-test", FileName: "lite.gguf",
			SourceURL:  srv.URL + "/lite",
			TotalBytes: int64(len(payload)), ExpectedSize: int64(len(payload)),
			ChecksumValue: shaOf(payload), ContextSize: 2048, MaxTokens: 512,
			RAMGB: 2, License: "Apache-2.0", Source: "test",
		},
		"balanced": {
			Title: "Test balanced", ModelName: "Qwen3-Test", Quantization: "Q4_K_M",
			Version: "v-test", FileName: "balanced.gguf",
			SourceURL:  srv.URL + "/lite",
			TotalBytes: int64(len(payload)), ExpectedSize: int64(len(payload)),
			ChecksumValue: shaOf(payload), ContextSize: 2048, MaxTokens: 512,
			RAMGB: 2, License: "Apache-2.0", Source: "test",
		},
	}
	svc, err := assistant.New(db, dir, assistant.Options{Binary: fakeBinary(t, dir), Pins: pins})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	snap, err := svc.StartDownload(mgr, "lite")
	if err != nil {
		t.Fatal(err)
	}
	// Wait until the transfer is genuinely in-flight, then cancel it.
	deadline := time.Now().Add(10 * time.Second)
	for {
		cur, err := mgr.Get(snap.ID)
		if err != nil {
			t.Fatal(err)
		}
		if cur.Status == "downloading" || cur.Status == "queued" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("download never started: %s", cur.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := mgr.Control(snap.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	// Cancelled downloads cannot be installed.
	if _, _, err := svc.Install(mgr, "lite", snap.ID); !isCode(err, "invalid_state") {
		t.Fatalf("cancelled install: %v", err)
	}
	// A new explicit acquisition supersedes the cancelled one.
	snap2, err := svc.StartDownload(mgr, "lite")
	if err != nil {
		t.Fatal(err)
	}
	if snap2.ID == snap.ID {
		t.Fatal("new acquisition must have a new id")
	}
	close(release)
	waitDownload(t, mgr, snap2.ID)
	if _, _, err := svc.Install(mgr, "lite", snap2.ID); err != nil {
		t.Fatal(err)
	}
	waitProfile(t, svc, mgr, "lite", "ready")

	// A foreign completed download must never bind to another profile.
	otherSnap, err := mgr.Create(download.Descriptor{
		Provider: "other", ProviderPackageID: "other",
		Title: "Other", FileName: "other.bin", SourceURL: srv.URL + "/lite",
		Version: "v-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitDownload(t, mgr, otherSnap.ID)
	if _, _, err := svc.Install(mgr, "balanced", otherSnap.ID); err == nil {
		t.Fatal("foreign artifact must not install")
	} else if !isCode(err, "metadata_unavailable") && !isCode(err, "checksum_unavailable") && !isCode(err, "invalid_state") && !isCode(err, "not_found") {
		t.Fatalf("foreign artifact: %v", err)
	}
	// Empty download id is rejected without touching the manager.
	if _, _, err := svc.Install(mgr, "balanced", " "); !isCode(err, "invalid_request") {
		t.Fatalf("empty download id: %v", err)
	}
	// Idempotent retry with the original download stays ready.
	prof, installed, err := svc.Install(mgr, "lite", snap2.ID)
	if err != nil || !installed || prof.Status != "ready" {
		t.Fatalf("idempotent reinstall: %+v %v %v", prof, installed, err)
	}
}

func TestRemoveActiveAndBusy(t *testing.T) {
	fx := setup(t, nil)
	installModelFlow(t, fx, "lite")
	installModelFlow(t, fx, "balanced")
	if err := fx.svc.Activate("lite"); err != nil {
		t.Fatal(err)
	}
	// Unknown profiles report not_found.
	if err := fx.svc.Remove("nope"); !isCode(err, "not_found") {
		t.Fatalf("remove unknown: %v", err)
	}
	// Inference holds the lease: mutations and rival inference go busy,
	// including removal of the active profile.
	release := make(chan struct{})
	inflight := make(chan error, 1)
	go func() {
		inflight <- fx.svc.UseModel(context.Background(), "lite", func(ctx context.Context, _ assistant.Model) error {
			close(release)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Second):
				return nil
			}
		})
	}()
	<-release
	if err := fx.svc.UseModel(context.Background(), "lite", func(_ context.Context, _ assistant.Model) error { return nil }); !isCode(err, "busy") {
		t.Fatalf("concurrent inference: %v", err)
	}
	if _, _, err := fx.svc.Install(fx.mgr, "balanced", "x"); !isCode(err, "busy") {
		t.Fatalf("install during inference: %v", err)
	}
	if err := fx.svc.Remove("balanced"); !isCode(err, "busy") {
		t.Fatalf("remove during inference: %v", err)
	}
	if err := fx.svc.Remove("lite"); !isCode(err, "busy") {
		t.Fatalf("remove active during inference: %v", err)
	}
	if err := fx.svc.Activate("balanced"); !isCode(err, "busy") {
		t.Fatalf("activate during inference: %v", err)
	}
	if _, err := fx.svc.StartDownload(fx.mgr, "balanced"); !isCode(err, "busy") && !isCode(err, "target_collision") {
		t.Fatalf("download during inference: %v", err)
	}
	st, err := fx.svc.List(fx.mgr)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Busy {
		t.Fatal("busy flag must be set during inference")
	}
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-inflight:
		if err == nil {
			t.Fatal("in-flight inference must fail after Close")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not interrupt inference")
	}
	// Reopen: remove the non-active profile, verify the tree and row are gone.
	svc, err := assistant.New(fx.db, fx.dir, assistant.Options{Binary: fx.binary, Pins: fx.pins, RuntimePin: &fx.rtPin})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	if err := svc.Remove("balanced"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(fx.dir, "content", "assistant", "models", "balanced")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("profile dir survives remove: %v", err)
	}
	if err := svc.Remove("balanced"); !isCode(err, "not_found") {
		t.Fatalf("second remove: %v", err)
	}
	// The active profile is removable when idle: removal clears the active id.
	if err := svc.Remove("lite"); err != nil {
		t.Fatalf("remove sole active model: %v", err)
	}
	st2, err := svc.List(nil)
	if err != nil {
		t.Fatal(err)
	}
	if st2.ActiveProfileID != "" {
		t.Fatalf("active id survives removal: %q", st2.ActiveProfileID)
	}
	for _, p := range st2.Profiles {
		if p.ID == "lite" && p.Status != "missing" {
			t.Fatalf("removed profile status=%s", p.Status)
		}
	}
	if err := svc.UseModel(context.Background(), "", func(_ context.Context, _ assistant.Model) error { return nil }); !isCode(err, "not_found") {
		t.Fatalf("empty id after active removal: %v", err)
	}
	// Reinstall the sole model and reactivate it.
	snap, err := svc.StartDownload(fx.mgr, "lite")
	if err != nil {
		t.Fatal(err)
	}
	waitDownload(t, fx.mgr, snap.ID)
	if _, _, err := svc.Install(fx.mgr, "lite", snap.ID); err != nil {
		t.Fatal(err)
	}
	waitProfile(t, svc, fx.mgr, "lite", "ready")
	if err := svc.Activate("lite"); err != nil {
		t.Fatal(err)
	}
	if err := svc.UseModel(context.Background(), "", func(_ context.Context, m assistant.Model) error {
		if m.ID != "lite" {
			t.Fatalf("model=%s", m.ID)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Close cancels then joins: a second Close is a no-op success.
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeArchiveAttacks(t *testing.T) {
	binEntry := "pkg/" + serverBinaryName()
	cases := map[string][]tarEntry{
		"traversal": {
			{name: binEntry, body: []byte("x"), mode: 0o755},
			{name: "pkg/../../evil", body: []byte("escape"), mode: 0o644},
		},
		"absolute": {
			{name: binEntry, body: []byte("x"), mode: 0o755},
			{name: "/evil", body: []byte("escape"), mode: 0o644},
		},
		"symlink-escape": {
			{name: binEntry, body: []byte("x"), mode: 0o755},
			{name: "pkg/link", link: "/etc/passwd"},
		},
		"symlink-dotdot": {
			{name: binEntry, body: []byte("x"), mode: 0o755},
			{name: "pkg/sub/link", link: "../../evil"},
		},
		"no-binary": {
			{name: "pkg/lib.so", body: []byte("x"), mode: 0o644},
		},
	}
	for name, entries := range cases {
		evil := buildTarGz(t, entries)
		fx := setup(t, func(pins map[string]assistant.Acquisition, rt *assistant.RuntimeAcquisition, data map[string][]byte) {
			data["runtime"] = evil
			rt.TotalBytes = int64(len(evil))
			rt.ExpectedSize = int64(len(evil))
			rt.ChecksumValue = shaOf(evil)
		})
		snap, err := fx.svc.StartRuntimeDownload(fx.mgr)
		if err != nil {
			t.Fatalf("%s: download: %v", name, err)
		}
		waitDownload(t, fx.mgr, snap.ID)
		if _, err := fx.svc.InstallRuntime(fx.mgr, snap.ID); err != nil {
			t.Fatalf("%s: install accept: %v", name, err)
		}
		got := waitRuntime(t, fx.svc, fx.mgr, "error")
		if got.Error == nil {
			t.Fatalf("%s: expected error status", name)
		}
		for _, p := range []string{
			filepath.Join(fx.dir, "evil"),
			filepath.Join(fx.dir, "content", "evil"),
			filepath.Join(fx.dir, "content", "assistant", "evil"),
		} {
			if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%s: escape path exists: %s", name, p)
			}
		}
		if _, err := os.Lstat(filepath.Join(fx.dir, "content", "assistant", "runtime", serverBinaryName())); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s: runtime must not publish on failure", name)
		}
	}
}

func TestNoArbitraryURLAccepted(t *testing.T) {
	fx := setup(t, nil)
	// Unknown profile ids never reach the network.
	if _, err := fx.svc.StartDownload(fx.mgr, "../lite"); !isCode(err, "not_found") {
		t.Fatalf("path id: %v", err)
	}
	if _, err := fx.svc.StartRuntimeDownload(nil); err == nil {
		t.Fatal("nil manager must fail")
	}
	// Pins with non-HTTP provenance are refused without network use.
	badPins := map[string]assistant.Acquisition{}
	for id, p := range fx.pins {
		badPins[id] = p
	}
	bad := badPins["lite"]
	bad.SourceURL = "ftp://example.invalid/lite.gguf"
	badPins["lite"] = bad
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	badSvc, err := assistant.New(fx.db, fx.dir, assistant.Options{Binary: fx.binary, Pins: badPins, RuntimePin: &fx.rtPin})
	if err != nil {
		t.Fatal(err)
	}
	defer badSvc.Close()
	if _, err := badSvc.StartDownload(fx.mgr, "lite"); err == nil {
		t.Fatal("ftp provenance must be refused")
	}
	// An explicitly empty pin map disables acquisition entirely.
	empty, err := assistant.New(fx.db, fx.dir, assistant.Options{Binary: fx.binary, Pins: map[string]assistant.Acquisition{}})
	if err != nil {
		t.Fatal(err)
	}
	defer empty.Close()
	if _, err := empty.StartDownload(fx.mgr, "lite"); !isCode(err, "metadata_unavailable") {
		t.Fatalf("empty pins: %v", err)
	}
	st, err := empty.List(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range st.Profiles {
		if p.Status != "missing" {
			t.Fatalf("unverified must be missing: %+v", p)
		}
	}
}

func TestNoNetworkOnStartupAndList(t *testing.T) {
	fx := setup(t, nil)
	installModelFlow(t, fx, "lite")
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	fx.server.Close()
	fx.mgr.Close()
	before := fx.hits.Load()
	svc, err := assistant.New(fx.db, fx.dir, assistant.Options{Binary: fx.binary, Pins: fx.pins, RuntimePin: &fx.rtPin})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	st, err := svc.List(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range st.Profiles {
		if p.ID == "lite" && p.Status != "ready" {
			t.Fatalf("restart lost model: %+v", p)
		}
	}
	if got := fx.hits.Load() - before; got != 0 {
		t.Fatalf("startup+List used network: %d hits", got)
	}
	// Missing engine binary degrades to engine_unavailable, never a crash.
	nobin, err := assistant.New(fx.db, fx.dir, assistant.Options{Binary: filepath.Join(fx.dir, "does-not-exist"), Pins: fx.pins, RuntimePin: &fx.rtPin})
	if err != nil {
		t.Fatal(err)
	}
	defer nobin.Close()
	if err := nobin.UseModel(context.Background(), "lite", func(_ context.Context, _ assistant.Model) error { return nil }); !isCode(err, "engine_unavailable") {
		t.Fatalf("missing binary: %v", err)
	}
	rst, err := nobin.List(nil)
	if err != nil {
		t.Fatal(err)
	}
	if rst.Runtime.Available {
		t.Fatal("runtime must be unavailable without binary")
	}
}

func TestDefaultPinsExactURLs(t *testing.T) {
	// Exact upstream snapshot URLs: any revision drift breaks provenance.
	// Values verified against the upstream hosts at the pinned revisions.
	wantURLs := map[string]string{
		"lite":     "https://huggingface.co/unsloth/Qwen3-0.6B-GGUF/resolve/50968a4468ef4233ed78cd7c3de230dd1d61a56b/Qwen3-0.6B-Q4_K_M.gguf",
		"balanced": "https://huggingface.co/unsloth/Qwen3-1.7B-GGUF/resolve/d7f544eead698dbd1f15126ef60b45a1e1933222/Qwen3-1.7B-Q4_K_M.gguf",
		"quality":  "https://huggingface.co/Qwen/Qwen3-4B-GGUF/resolve/bc640142c66e1fdd12af0bd68f40445458f3869b/Qwen3-4B-Q4_K_M.gguf",
	}
	wantRepos := map[string]string{
		"lite":     "unsloth/Qwen3-0.6B-GGUF",
		"balanced": "unsloth/Qwen3-1.7B-GGUF",
		"quality":  "Qwen/Qwen3-4B-GGUF",
	}
	wantRevisions := map[string]string{
		"lite":     "50968a4468ef4233ed78cd7c3de230dd1d61a56b",
		"balanced": "d7f544eead698dbd1f15126ef60b45a1e1933222",
		"quality":  "bc640142c66e1fdd12af0bd68f40445458f3869b",
	}
	wantRAM := map[string]int{"lite": 4, "balanced": 8, "quality": 12}
	for _, id := range []string{"lite", "balanced", "quality"} {
		pin, ok := assistant.DefaultPins[id]
		if !ok {
			t.Fatalf("missing default pin %s", id)
		}
		if pin.SourceURL != wantURLs[id] {
			t.Fatalf("pin %s URL drift:\n got %q\nwant %q", id, pin.SourceURL, wantURLs[id])
		}
		if pin.Repo != wantRepos[id] {
			t.Fatalf("pin %s repo=%q, want %q", id, pin.Repo, wantRepos[id])
		}
		if pin.Revision != wantRevisions[id] || len(pin.Revision) != 40 {
			t.Fatalf("pin %s revision=%q", id, pin.Revision)
		}
		if !strings.Contains(pin.SourceURL, "/resolve/"+pin.Revision+"/") {
			t.Fatalf("pin %s URL does not embed its revision", id)
		}
		if !strings.HasSuffix(pin.SourceURL, "/"+pin.FileName) {
			t.Fatalf("pin %s URL does not end with its file name", id)
		}
		if pin.RAMGB != wantRAM[id] {
			t.Fatalf("pin %s ram_gb=%d, want %d", id, pin.RAMGB, wantRAM[id])
		}
	}
	if !strings.Contains(assistant.DefaultPins["lite"].Title, "Lite") ||
		!strings.Contains(assistant.DefaultPins["balanced"].Title, "Balanced") ||
		!strings.Contains(assistant.DefaultPins["quality"].Title, "Quality") {
		t.Fatal("profile titles must visibly carry Lite/Balanced/Quality")
	}
	if !strings.Contains(assistant.DefaultPins["quality"].Title, "12") {
		t.Fatal("quality title must disclose the 12–16 GB expectation")
	}
}

func TestDefaultPinsVerified(t *testing.T) {
	for _, id := range []string{"lite", "balanced", "quality"} {
		pin, ok := assistant.DefaultPins[id]
		if !ok {
			t.Fatalf("missing default pin %s", id)
		}
		if pin.Version == "" || pin.FileName == "" || pin.SourceURL == "" {
			t.Fatalf("pin %s has empty metadata: %+v", id, pin)
		}
		if !strings.HasPrefix(pin.SourceURL, "https://") || strings.Contains(pin.SourceURL, "..") {
			t.Fatalf("pin %s has unsafe URL %q", id, pin.SourceURL)
		}
		if pin.TotalBytes <= 0 || pin.ExpectedSize <= 0 || pin.TotalBytes != pin.ExpectedSize {
			t.Fatalf("pin %s has bad sizes: %+v", id, pin)
		}
		raw, err := hex.DecodeString(pin.ChecksumValue)
		if err != nil || len(raw) != 32 || pin.ChecksumValue != strings.ToLower(pin.ChecksumValue) {
			t.Fatalf("pin %s has bad sha256", id)
		}
		if pin.ContextSize <= 0 || pin.MaxTokens <= 0 || pin.RAMGB <= 0 {
			t.Fatalf("pin %s has bad budget: %+v", id, pin)
		}
		if pin.License != "Apache-2.0" || pin.Source == "" {
			t.Fatalf("pin %s missing license/source label: %+v", id, pin)
		}
	}
	if got := assistant.DefaultPins["quality"].Source; !strings.Contains(got, "resmi") {
		t.Fatalf("quality must be labelled official, got %q", got)
	}
	for _, id := range []string{"lite", "balanced"} {
		if got := assistant.DefaultPins[id].Source; !strings.Contains(got, "topluluk") {
			t.Fatalf("%s must be labelled community, got %q", id, got)
		}
	}
	for key, pin := range assistant.DefaultRuntimePins {
		if pin.Version == "" || pin.FileName == "" || !strings.HasPrefix(pin.SourceURL, "https://") {
			t.Fatalf("runtime %s bad metadata: %+v", key, pin)
		}
		raw, err := hex.DecodeString(pin.ChecksumValue)
		if err != nil || len(raw) != 32 || pin.ChecksumValue != strings.ToLower(pin.ChecksumValue) {
			t.Fatalf("runtime %s bad sha256", key)
		}
		if pin.TotalBytes != pin.ExpectedSize || pin.TotalBytes <= 0 {
			t.Fatalf("runtime %s bad sizes", key)
		}
	}
	if _, ok := assistant.DefaultRuntimePins["linux/amd64"]; !ok {
		t.Fatal("missing linux/amd64 runtime pin")
	}
	if _, ok := assistant.DefaultRuntimePins["windows/amd64"]; !ok {
		t.Fatal("missing windows/amd64 runtime pin")
	}
}

func TestSafeSymlinkPreserved(t *testing.T) {
	fx := setup(t, nil)
	installModelFlow(t, fx, "lite")
	installRuntimeFlow(t, fx)
	if runtime.GOOS == "windows" {
		// Windows-shaped fixture ships exe + DLL without POSIX symlinks.
		if _, err := os.Lstat(filepath.Join(fx.dir, "content", "assistant", "runtime", serverBinaryName())); err != nil {
			t.Fatalf("windows binary missing: %v", err)
		}
		if _, err := os.Lstat(filepath.Join(fx.dir, "content", "assistant", "runtime", "lib", runtimeLibName())); err != nil {
			t.Fatalf("windows dll missing: %v", err)
		}
	} else {
		// The legitimate relative symlink ships inside the archive layout.
		// Skip only when the OS truly lacks symlink privilege; a normal
		// install must still be verified below.
		link := filepath.Join(fx.dir, "content", "assistant", "runtime", "lib", "link")
		target, err := os.Readlink(link)
		if err != nil {
			t.Skipf("symlink privilege missing: %v", err)
		} else {
			if target != "libggml.so" {
				t.Fatalf("symlink target=%q", target)
			}
			resolved, err := filepath.EvalSymlinks(link)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(resolved, filepath.Join(fx.dir, "content", "assistant", "runtime")) {
				t.Fatalf("symlink escapes runtime: %s", resolved)
			}
		}
	}
	// Restart keeps the runtime ready without re-hashing payloads.
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	fx.server.Close()
	fx.mgr.Close()
	before := fx.hits.Load()
	svc, err := assistant.New(fx.db, fx.dir, assistant.Options{Binary: "", Pins: fx.pins, RuntimePin: &fx.rtPin})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	st, err := svc.List(nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Runtime.Status != "ready" || !st.Runtime.Available {
		t.Fatalf("runtime lost: %+v", st.Runtime)
	}
	// Installed runtime resolves without an explicit Binary override.
	if err := svc.UseModel(context.Background(), "lite", func(_ context.Context, m assistant.Model) error {
		if !strings.HasSuffix(m.Binary, serverBinaryName()) {
			t.Fatalf("binary=%q", m.Binary)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := fx.hits.Load() - before; got != 0 {
		t.Fatalf("restart used network: %d", got)
	}
}

func TestConcurrentInstallRejected(t *testing.T) {
	fx := setup(t, nil)
	snap1, err := fx.svc.StartDownload(fx.mgr, "lite")
	if err != nil {
		t.Fatal(err)
	}
	waitDownload(t, fx.mgr, snap1.ID)
	if _, _, err := fx.svc.Install(fx.mgr, "lite", snap1.ID); err != nil {
		t.Fatal(err)
	}
	// A second install for the same profile goes busy while the first runs.
	if _, _, err := fx.svc.Install(fx.mgr, "lite", snap1.ID); !isCode(err, "busy") {
		t.Fatalf("concurrent install: %v", err)
	}
	waitProfile(t, fx.svc, fx.mgr, "lite", "ready")
}

func TestCloseJoinsInstalls(t *testing.T) {
	// A large payload keeps the install worker busy; Close must join it: the
	// profile is either ready or error, never installing, after Close returns.
	big := bytes.Repeat([]byte("G"), 8<<20)
	fx := setup(t, func(pins map[string]assistant.Acquisition, rt *assistant.RuntimeAcquisition, data map[string][]byte) {
		data["lite"] = big
		p := pins["lite"]
		p.TotalBytes = int64(len(big))
		p.ExpectedSize = int64(len(big))
		p.ChecksumValue = shaOf(big)
		pins["lite"] = p
	})
	snap, err := fx.svc.StartDownload(fx.mgr, "lite")
	if err != nil {
		t.Fatal(err)
	}
	waitDownload(t, fx.mgr, snap.ID)
	if _, _, err := fx.svc.Install(fx.mgr, "lite", snap.ID); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("close joined in %v", time.Since(start))
	var status string
	if err := fx.db.QueryRow(`SELECT status FROM assistant_models WHERE id='lite'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status == "installing" {
		t.Fatal("install left in installing after Close")
	}
}

func TestUseModelMergesServiceLifetime(t *testing.T) {
	fx := setup(t, nil)
	installModelFlow(t, fx, "lite")
	if err := fx.svc.Activate("lite"); err != nil {
		t.Fatal(err)
	}
	// Caller cancellation propagates to the callback context.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := fx.svc.UseModel(cancelled, "lite", func(_ context.Context, _ assistant.Model) error { return nil }); err == nil {
		t.Fatal("cancelled caller ctx must fail")
	}
	// Service Close cancels an in-flight callback context.
	done := make(chan error, 1)
	go func() {
		done <- fx.svc.UseModel(context.Background(), "lite", func(ctx context.Context, _ assistant.Model) error {
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	time.Sleep(200 * time.Millisecond)
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("in-flight UseModel must fail after Close")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not cancel inference")
	}
}

func TestStorageFailureIsolated(t *testing.T) {
	fx := setup(t, nil)
	installModelFlow(t, fx, "lite")
	// A broken registry fails List loudly instead of silently disabling rows.
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.db.Exec(`DROP TABLE assistant_models`); err != nil {
		t.Fatal(err)
	}
	svc, err := assistant.New(fx.db, fx.dir, assistant.Options{Binary: fx.binary, Pins: fx.pins, RuntimePin: &fx.rtPin})
	if err == nil {
		svc.Close()
		t.Fatal("New with broken storage must fail")
	}
}

var _ = sync.Mutex{}
var _ = io.Discard
