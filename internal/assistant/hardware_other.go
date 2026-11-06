//go:build !linux && !windows

package assistant

func platformMemory() (total, avail int64, ok bool) {
	return 0, 0, false
}
