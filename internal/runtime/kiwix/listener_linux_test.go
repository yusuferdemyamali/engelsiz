package kiwix

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProcOwnershipEvidenceFailsClosed(t *testing.T) {
	root := t.TempDir()
	pidRoot := filepath.Join(root, "123")
	fdRoot := filepath.Join(pidRoot, "fd")
	tcp := filepath.Join(pidRoot, "net", "tcp")
	assertClosed := func() {
		t.Helper()
		if owned, err := childOwnsListenerInProc(root, 123, 17321); owned || err == nil {
			t.Fatalf("unavailable/malformed procfs must fail closed: %v %v", owned, err)
		}
	}
	assertClosed() // missing child/procfs
	if err := os.MkdirAll(fdRoot, 0700); err != nil {
		t.Fatal(err)
	}
	assertClosed() // missing child's network table
	if err := os.MkdirAll(filepath.Dir(tcp), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tcp, []byte("not a TCP table\n"), 0600); err != nil {
		t.Fatal(err)
	}
	assertClosed()
	if err := os.WriteFile(tcp, []byte("sl local_address rem_address\ntruncated\n"), 0600); err != nil {
		t.Fatal(err)
	}
	assertClosed()
	if err := os.Symlink("socket:[not-an-inode]", filepath.Join(fdRoot, "3")); err != nil {
		t.Fatal(err)
	}
	assertClosed()
}
