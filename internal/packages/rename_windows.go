//go:build windows

package packages

import (
	"golang.org/x/sys/windows"
	"os"
	"unsafe"
)

func renameNoReplace(source *os.Root, old string, target *os.Root, new string) error {
	from, e := source.Open(".")
	if e != nil {
		return e
	}
	defer from.Close()
	to, e := target.Open(".")
	if e != nil {
		return e
	}
	defer to.Close()
	name, e := windows.NewNTUnicodeString(old)
	if e != nil {
		return e
	}
	attr := windows.OBJECT_ATTRIBUTES{RootDirectory: windows.Handle(from.Fd()), ObjectName: name, Attributes: windows.OBJ_CASE_INSENSITIVE}
	attr.Length = uint32(unsafe.Sizeof(attr))
	var handle windows.Handle
	var ios windows.IO_STATUS_BLOCK
	e = windows.NtCreateFile(&handle, windows.DELETE|windows.SYNCHRONIZE, &attr, &ios, nil, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_OPEN, windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	if e != nil {
		return e
	}
	defer windows.CloseHandle(handle)
	utf, e := windows.UTF16FromString(new)
	if e != nil {
		return e
	}
	utf = utf[:len(utf)-1]
	type renameInfo struct {
		Replace uint32
		Root    windows.Handle
		Length  uint32
		Name    [1]uint16
	}
	var layout renameInfo
	size := int(unsafe.Offsetof(layout.Name)) + len(utf)*2
	buffer := make([]byte, size)
	info := (*renameInfo)(unsafe.Pointer(&buffer[0]))
	info.Root = windows.Handle(to.Fd())
	info.Length = uint32(len(utf) * 2)
	copy(unsafe.Slice(&info.Name[0], len(utf)), utf)
	// Replace is zero: FileRenameInformation must refuse any existing target.
	e = windows.NtSetInformationFile(handle, &ios, &buffer[0], uint32(size), windows.FileRenameInformation)
	if e == windows.STATUS_OBJECT_NAME_COLLISION {
		return os.ErrExist
	}
	return e
}
