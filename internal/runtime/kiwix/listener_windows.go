package kiwix

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var getExtendedTCPTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

func childOwnsListener(pid, port int) (bool, error) {
	if pid <= 0 || port <= 0 || port > 65535 {
		return false, fmt.Errorf("invalid child PID or listener port")
	}
	if err := getExtendedTCPTable.Find(); err != nil {
		return false, fmt.Errorf("find GetExtendedTcpTable: %w", err)
	}
	var size uint32
	// AF_INET=2; TCP_TABLE_OWNER_PID_LISTENER=3. The table can grow between
	// size and data queries; retry a bounded number of times, then fail closed.
	var table []byte
	for range 3 {
		var ptr unsafe.Pointer
		if len(table) > 0 {
			ptr = unsafe.Pointer(&table[0])
		}
		code, _, _ := getExtendedTCPTable.Call(uintptr(ptr), uintptr(unsafe.Pointer(&size)), 0, 2, 3, 0)
		runtime.KeepAlive(table)
		if code == 0 && len(table) > 0 {
			if size > uint32(len(table)) {
				return false, fmt.Errorf("invalid TCP table size")
			}
			return windowsListenerOwner(table[:size], pid, port)
		}
		if code != uintptr(windows.ERROR_INSUFFICIENT_BUFFER) {
			return false, fmt.Errorf("GetExtendedTcpTable: %w", windows.Errno(code))
		}
		if size < 4 || size > 16*1024*1024 {
			return false, fmt.Errorf("invalid TCP table allocation size %d", size)
		}
		table = make([]byte, int(size))
	}
	return false, fmt.Errorf("GetExtendedTcpTable changed during bounded query")
}
