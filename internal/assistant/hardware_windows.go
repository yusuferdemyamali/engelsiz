//go:build windows

package assistant

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// memoryStatusEx mirrors the Win32 MEMORYSTATUSEX layout. It is filled by
// kernel32 GlobalMemoryStatusEx through a lazy DLL load: no subprocess, no
// network, no cgo.
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func platformMemory() (total, avail int64, ok bool) {
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")
	var st memoryStatusEx
	st.Length = uint32(unsafe.Sizeof(st))
	ret, _, _ := proc.Call(uintptr(unsafe.Pointer(&st)))
	if ret == 0 {
		return 0, 0, false
	}
	return int64(st.TotalPhys), int64(st.AvailPhys), true
}
