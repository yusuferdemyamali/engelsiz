package kiwix

import (
	"encoding/binary"
	"fmt"
)

// MIB_TCPTABLE_OWNER_PID starts with a DWORD row count; each
// MIB_TCPROW_OWNER_PID contains six DWORDs. Windows DWORDs are little endian,
// except address/port bytes, which the API documents in network byte order.
// Kept platform-independent so malformed/mismatched API data is testable on Linux.
func windowsListenerOwner(table []byte, pid, port int) (bool, error) {
	if len(table) < 4 {
		return false, fmt.Errorf("truncated PID-owner TCP table")
	}
	rows := uint64(binary.LittleEndian.Uint32(table[:4]))
	if rows > uint64((len(table)-4)/24) {
		return false, fmt.Errorf("truncated PID-owner TCP rows")
	}
	for i := uint64(0); i < rows; i++ {
		row := table[4+i*24 : 4+(i+1)*24]
		if binary.LittleEndian.Uint32(row[0:4]) == 2 && // MIB_TCP_STATE_LISTEN
			row[4] == 127 && row[5] == 0 && row[6] == 0 && row[7] == 1 &&
			int(binary.BigEndian.Uint16(row[8:10])) == port &&
			uint64(binary.LittleEndian.Uint32(row[20:24])) == uint64(pid) {
			return true, nil
		}
	}
	return false, nil
}
