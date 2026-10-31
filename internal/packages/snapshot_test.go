package packages

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInstalledSnapshotCommitNotificationAndIdentity(t *testing.T) {
	committed := make(chan struct{}, 1)
	var f *fixture
	f = newFixture(t, Options{OnInstalled: func() {
		if _, err := f.s.InstalledSnapshot(); err != nil {
			t.Error(err)
		}
		committed <- struct{}{}
	}})
	if files, err := f.s.InstalledSnapshot(); err != nil || len(files) != 0 {
		t.Fatalf("before commit=%v %v", files, err)
	}
	if _, _, err := f.s.Install(f.m, testID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-committed:
	case <-time.After(5 * time.Second):
		t.Fatal("no commit notification")
	}
	files, err := f.s.InstalledSnapshot()
	if err != nil || len(files) != 1 || files[0].PackageID != testID {
		t.Fatalf("snapshot=%v %v", files, err)
	}

	f.s.Close()
	reopened, err := New(context.Background(), f.db, f.dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	files, err = reopened.InstalledSnapshot()
	if err != nil || len(files) != 1 {
		t.Fatalf("reconciled=%v %v", files, err)
	}
	path := files[0].Path
	if err = os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, f.body, 0600); err != nil {
		t.Fatal(err)
	}
	if files, err = reopened.InstalledSnapshot(); err != nil || len(files) != 0 {
		t.Fatalf("replaced inode trusted: %v %v", files, err)
	}
	// Unregistered files never become runtime inputs.
	if err = os.WriteFile(filepath.Join(f.dir, "content", "kiwix", "foreign.zim"), f.body, 0600); err != nil {
		t.Fatal(err)
	}
	if files, err = reopened.InstalledSnapshot(); err != nil || len(files) != 0 {
		t.Fatalf("scanned foreign=%v %v", files, err)
	}
}
