package assistant

import (
	"runtime"
)

// Hardware is the low-cost host snapshot exposed in Status. Memory values are
// best-effort: unknown platforms report zero memory with the real CPU count.
// No subprocess or network access is used.
type Hardware struct {
	TotalMemoryBytes     int64 `json:"total_memory_bytes"`
	AvailableMemoryBytes int64 `json:"available_memory_bytes"`
	CPUCount             int   `json:"cpu_count"`
}

func detectHardware() Hardware {
	hw := Hardware{CPUCount: runtime.NumCPU()}
	if hw.CPUCount < 1 {
		hw.CPUCount = 1
	}
	if total, avail, ok := platformMemory(); ok {
		hw.TotalMemoryBytes = total
		hw.AvailableMemoryBytes = avail
	}
	return hw
}

// recommendedProfile picks the richest profile fitting comfortably in the
// observed memory. Balanced is the default once the host has 8 GiB of total
// memory; below that Lite keeps low-end hardware usable. Quality is only ever
// recommended on clearly high-memory hosts (16 GiB or more) and stays a manual
// opt-in everywhere else. Unknown memory falls back to Lite.
func recommendedProfile(hw Hardware, pins map[string]Acquisition) string {
	return recommendFor(hw, Definitions(pins))
}

func recommendFor(hw Hardware, defs []Definition) string {
	const gb = int64(1) << 30
	total := hw.TotalMemoryBytes
	if total <= 0 {
		total = hw.AvailableMemoryBytes
	}
	byID := map[string]Definition{}
	for _, d := range defs {
		byID[d.ID] = d
	}
	if q, ok := byID[ProfileQuality]; ok && q.Verified() && total >= 16*gb {
		return ProfileQuality
	}
	if b, ok := byID[ProfileBalanced]; ok && b.Verified() && total >= 8*gb {
		return ProfileBalanced
	}
	return ProfileLite
}
