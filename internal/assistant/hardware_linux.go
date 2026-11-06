//go:build linux

package assistant

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

func platformMemory() (total, avail int64, ok bool) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	var memTotal, memAvail int64
	var haveTotal, haveAvail bool
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4096), 4096)
	for sc.Scan() {
		line := sc.Text()
		key, rest, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 2 || fields[1] != "kB" {
			continue
		}
		n, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil || n < 0 {
			continue
		}
		switch key {
		case "MemTotal":
			memTotal, haveTotal = n*1024, true
		case "MemAvailable":
			memAvail, haveAvail = n*1024, true
		}
	}
	if !haveTotal {
		return 0, 0, false
	}
	if !haveAvail {
		memAvail = memTotal
	}
	return memTotal, memAvail, true
}
