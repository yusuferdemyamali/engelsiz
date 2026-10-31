package kiwix

import (
	"encoding/binary"
	"net"
	"os"
	"testing"

	fixture "engelsiz/internal/testsupport/kiwixfixture"
)

func TestChildListenerOwnership(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	if owned, err := childOwnsListener(os.Getpid(), port); err != nil || !owned {
		t.Fatalf("own real listener: owned=%v err=%v", owned, err)
	}
	other, _ := setup(t, fixture.Config{})
	if err := other.EnsureStarted(t.Context()); err != nil {
		t.Fatal(err)
	}
	if owned, err := childOwnsListener(other.Status().PID, port); err != nil || owned {
		t.Fatalf("TCP table includes our listener but other child has no matching FD: owned=%v err=%v", owned, err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if owned, err := childOwnsListener(os.Getpid(), port); err != nil || owned {
		t.Fatalf("closed listener qualified: owned=%v err=%v", owned, err)
	}
	wildcard, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer wildcard.Close()
	if owned, err := childOwnsListener(os.Getpid(), wildcard.Addr().(*net.TCPAddr).Port); err != nil || owned {
		t.Fatalf("wildcard address qualified as exact loopback: owned=%v err=%v", owned, err)
	}
	if owned, err := childOwnsListener(0, port); owned || err == nil {
		t.Fatalf("invalid PID did not fail closed: owned=%v err=%v", owned, err)
	}
}

func TestWindowsListenerTableEvidence(t *testing.T) {
	// Documented MIB_TCPROW_OWNER_PID, not host-language struct padding.
	row := make([]byte, 28)
	binary.LittleEndian.PutUint32(row[0:4], 1)
	binary.LittleEndian.PutUint32(row[4:8], 2)
	copy(row[8:12], []byte{127, 0, 0, 1})
	binary.BigEndian.PutUint16(row[12:14], 17321)
	binary.LittleEndian.PutUint32(row[24:28], 1234)
	if owned, err := windowsListenerOwner(row, 1234, 17321); err != nil || !owned {
		t.Fatalf("documented listener row: %v %v", owned, err)
	}
	for _, tc := range []struct {
		name  string
		at    int
		value byte
	}{
		{"non-listening", 4, 5}, {"wildcard", 8, 0}, {"other loopback", 11, 2},
		{"other port", 12, 1}, {"other PID", 24, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := append([]byte(nil), row...)
			data[tc.at] = tc.value
			if owned, err := windowsListenerOwner(data, 1234, 17321); err != nil || owned {
				t.Fatalf("mismatch accepted: %v %v", owned, err)
			}
		})
	}
	for _, data := range [][]byte{nil, row[:3], row[:27], {255, 255, 255, 255}} {
		if owned, err := windowsListenerOwner(data, 1234, 17321); owned || err == nil {
			t.Fatalf("malformed evidence accepted: %v %v", owned, err)
		}
	}
}
