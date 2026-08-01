package discovery

import (
	gopsprocess "github.com/shirou/gopsutil/v4/process"
)

// RSSBytes returns the resident set size (physical memory currently in use)
// of the process with the given OS pid, in bytes. Uses gopsutil so this works
// identically on macOS, Linux, and Windows — a local chat model instance can
// be llama.cpp (native binary) or mlx_lm.server (Python), and shelling out to
// `ps`/`tasklist` per-OS would need separate parsing for each.
func RSSBytes(pid int) (uint64, error) {
	proc, err := gopsprocess.NewProcess(int32(pid))
	if err != nil {
		return 0, err
	}
	mem, err := proc.MemoryInfo()
	if err != nil {
		return 0, err
	}
	return mem.RSS, nil
}
