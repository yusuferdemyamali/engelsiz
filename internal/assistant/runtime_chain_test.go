package assistant_test

import (
	"engelsiz/internal/assistant"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestChainedSymlinkForwardOrder reproduces the real llama-b12174 CPU
// archive shape: safe chained relative symlinks whose parent link appears
// BEFORE its target in tar order (e.g. libmtmd.so -> libmtmd.so.0 while
// libmtmd.so.0 itself is a later member; libggml-base.so.0 ->
// libggml-base.so.0.29.0 while the real file comes later). Install must
// succeed; every intermediate and final link must resolve inside runtime.
func TestChainedSymlinkForwardOrder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix symlink chain fixture")
	}
	bin := serverBinaryName()
	entries := []tarEntry{
		{name: "llama-b12174/"},
		{name: "llama-b12174/" + bin, body: []byte("#!/bin/sh\necho hi\n"), mode: 0o755},
		// Forward references: parent link before its target exists.
		{name: "llama-b12174/libmtmd.so", link: "libmtmd.so.0"},
		{name: "llama-b12174/libggml-base.so.0", link: "libggml-base.so.0.29.0"},
		{name: "llama-b12174/libmtmd.so.0.9.0", body: []byte("fake-mtmd-real"), mode: 0o644},
		{name: "llama-b12174/libmtmd.so.0", link: "libmtmd.so.0.9.0"},
		{name: "llama-b12174/libggml-base.so.0.29.0", body: []byte("fake-ggml-base-real"), mode: 0o644},
		{name: "llama-b12174/libggml-base.so", link: "libggml-base.so.0"},
		{name: "llama-b12174/LICENSE", body: []byte("MIT"), mode: 0o644},
	}
	shuffled := buildTarGz(t, entries)
	fx := setup(t, func(pins map[string]assistant.Acquisition, rt *assistant.RuntimeAcquisition, data map[string][]byte) {
		data["runtime"] = shuffled
		rt.TotalBytes = int64(len(shuffled))
		rt.ExpectedSize = int64(len(shuffled))
		rt.ChecksumValue = shaOf(shuffled)
	})
	installModelFlow(t, fx, "lite")
	installRuntimeFlow(t, fx)
	// Every chain link must resolve inside the runtime tree.
	for _, link := range []string{"libmtmd.so", "libmtmd.so.0", "libggml-base.so", "libggml-base.so.0"} {
		p := filepath.Join(fx.dir, "content", "assistant", "runtime", link)
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			t.Fatalf("chain link %s unresolved: %v", link, err)
		}
		if !strings.HasPrefix(resolved, filepath.Join(fx.dir, "content", "assistant", "runtime")) {
			t.Fatalf("chain link %s escapes: %s", link, resolved)
		}
	}
	if _, err := os.Lstat(filepath.Join(fx.dir, "content", "assistant", "runtime", bin)); err != nil {
		t.Fatalf("binary missing: %v", err)
	}
}
