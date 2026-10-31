package kiwix

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Readable procfs for our own child is required. The TCP table belongs to the
// child's network namespace, but only an inode also present in that child's
// open file descriptors proves ownership. Never infer it from the table alone.
func childOwnsListener(pid, port int) (bool, error) {
	return childOwnsListenerInProc("/proc", pid, port)
}

func childOwnsListenerInProc(proc string, pid, port int) (bool, error) {
	if pid <= 0 || port <= 0 || port > 65535 {
		return false, fmt.Errorf("invalid child PID or listener port")
	}
	root := filepath.Join(proc, strconv.Itoa(pid))
	fds, err := os.ReadDir(filepath.Join(root, "fd"))
	if err != nil {
		return false, fmt.Errorf("read child socket descriptors: %w", err)
	}
	inodes := make(map[string]bool)
	for _, fd := range fds {
		target, err := os.Readlink(filepath.Join(root, "fd", fd.Name()))
		if os.IsNotExist(err) { // a descriptor can close during the snapshot
			continue
		}
		if err != nil {
			return false, fmt.Errorf("read child descriptor: %w", err)
		}
		if strings.HasPrefix(target, "socket:[") && strings.HasSuffix(target, "]") {
			inode := strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")
			if _, err := strconv.ParseUint(inode, 10, 64); err != nil {
				return false, fmt.Errorf("invalid child socket inode: %w", err)
			}
			inodes[inode] = true
		}
	}
	f, err := os.Open(filepath.Join(root, "net", "tcp"))
	if err != nil {
		return false, fmt.Errorf("read child IPv4 TCP table: %w", err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	if !s.Scan() || !strings.Contains(s.Text(), "local_address") {
		return false, fmt.Errorf("missing IPv4 TCP table header")
	}
	endpoint := fmt.Sprintf("%08X:%04X", binary.NativeEndian.Uint32([]byte{127, 0, 0, 1}), port)
	owned := false
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 10 {
			return false, fmt.Errorf("malformed IPv4 TCP table row")
		}
		if fields[1] == endpoint && fields[3] == "0A" && inodes[fields[9]] {
			owned = true
		}
	}
	if err := s.Err(); err != nil {
		return false, fmt.Errorf("scan IPv4 TCP table: %w", err)
	}
	return owned, nil
}
