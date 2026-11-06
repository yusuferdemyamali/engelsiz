package assistant_test

import (
	"archive/zip"
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"engelsiz/internal/assistant"
)

func buildZipNested(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		hdr := &zip.FileHeader{Name: name, Method: zip.Store}
		hdr.SetMode(0o644)
		if strings.HasSuffix(name, serverBinaryName()) {
			hdr.SetMode(0o755)
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func nestedTarEntries() []tarEntry {
	bin := serverBinaryName()
	lib := runtimeLibName()
	return []tarEntry{
		{name: "bin/" + bin, body: []byte("MZ-fake-nested"), mode: 0o755},
		{name: "lib/" + lib, body: []byte("fake-lib-nested"), mode: 0o644},
	}
}

func TestNestedRuntimeTarResolves(t *testing.T) {
	nested := buildTarGz(t, nestedTarEntries())
	fx := setup(t, func(pins map[string]assistant.Acquisition, rt *assistant.RuntimeAcquisition, data map[string][]byte) {
		data["runtime"] = nested
		rt.TotalBytes = int64(len(nested))
		rt.ExpectedSize = int64(len(nested))
		rt.ChecksumValue = shaOf(nested)
	})
	installModelFlow(t, fx, "lite")
	installRuntimeFlow(t, fx)
	// Reopen without explicit custom binary: trusted path must come from receipt.
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	svc, err := assistant.New(fx.db, fx.dir, assistant.Options{Pins: fx.pins, RuntimePin: &fx.rtPin})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	st, err := svc.List(nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Runtime.Status != "ready" || !st.Runtime.Available {
		t.Fatalf("nested tar runtime not ready: %+v", st.Runtime)
	}
	want := filepath.Join(fx.dir, "content", "assistant", "runtime", "bin", serverBinaryName())
	if err := svc.UseModel(context.Background(), "lite", func(_ context.Context, m assistant.Model) error {
		if m.Binary != want {
			t.Fatalf("binary=%q want %q", m.Binary, want)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Close/New round-trip keeps the nested binding.
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	svc2, err := assistant.New(fx.db, fx.dir, assistant.Options{Pins: fx.pins, RuntimePin: &fx.rtPin})
	if err != nil {
		t.Fatal(err)
	}
	defer svc2.Close()
	st2, err := svc2.List(nil)
	if err != nil {
		t.Fatal(err)
	}
	if st2.Runtime.Status != "ready" || !st2.Runtime.Available {
		t.Fatalf("after reopen: %+v", st2.Runtime)
	}
	if err := svc2.UseModel(context.Background(), "lite", func(_ context.Context, m assistant.Model) error {
		if m.Binary != want {
			t.Fatalf("reopen binary=%q want %q", m.Binary, want)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestNestedRuntimeZipResolves(t *testing.T) {
	// Windows-shaped zip: exe + DLL (no symlinks); POSIX: server + .so.
	nestedZip := buildZipNested(t, map[string][]byte{
		"bin/" + serverBinaryName(): []byte("MZ-fake-nested-zip"),
		"lib/" + runtimeLibName():  []byte("fake-lib-zip"),
	})
	fx := setup(t, func(pins map[string]assistant.Acquisition, rt *assistant.RuntimeAcquisition, data map[string][]byte) {
		data["runtime"] = nestedZip
		rt.TotalBytes = int64(len(nestedZip))
		rt.ExpectedSize = int64(len(nestedZip))
		rt.ChecksumValue = shaOf(nestedZip)
		rt.Format = "zip"
	})
	installModelFlow(t, fx, "lite")
	installRuntimeFlow(t, fx)
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	svc, err := assistant.New(fx.db, fx.dir, assistant.Options{Pins: fx.pins, RuntimePin: &fx.rtPin})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	st, err := svc.List(nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Runtime.Status != "ready" || !st.Runtime.Available {
		t.Fatalf("nested zip runtime not ready: %+v", st.Runtime)
	}
	want := filepath.Join(fx.dir, "content", "assistant", "runtime", "bin", serverBinaryName())
	if err := svc.UseModel(context.Background(), "lite", func(_ context.Context, m assistant.Model) error {
		if m.Binary != want {
			t.Fatalf("binary=%q want %q", m.Binary, want)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCloseWaitsForInference(t *testing.T) {
	fx := setup(t, nil)
	installModelFlow(t, fx, "lite")
	started := make(chan struct{})
	finished := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- fx.svc.UseModel(context.Background(), "lite", func(ctx context.Context, _ assistant.Model) error {
			close(started)
			<-ctx.Done()
			// Deliberately keep running after cancellation: Close must block
			// until the callback returns.
			time.Sleep(400 * time.Millisecond)
			close(finished)
			return ctx.Err()
		})
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("inference never started")
	}
	// Give the lease a moment to settle, then Close must cancel and join.
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	select {
	case <-finished:
	default:
		t.Fatal("Close returned before inference callback finished")
	}
	if elapsed < 300*time.Millisecond {
		t.Fatalf("Close did not block for inference: %v", elapsed)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled inference must fail")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("inference did not return after Close")
	}
}
