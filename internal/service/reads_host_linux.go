//go:build linux

package service

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/Pegasus8/piworker/internal/hostsys"
)

// probeHost reads the host figures Linux shows cheaply: statfs of the data
// directory, /proc/meminfo and the thermal zones.
func probeHost(dataDir string) HostStatus {
	return linuxHost{proc: "/proc", sys: "/sys"}.probe(dataDir)
}

// linuxHost reads host figures below its /proc and /sys roots.
type linuxHost struct{ proc, sys string }

// maxProcFileBytes bounds what the probe reads of one file.
const maxProcFileBytes = 64 << 10

func (h linuxHost) probe(dataDir string) HostStatus {
	var st HostStatus
	if dataDir != "" {
		st.Disk = diskStatus(dataDir)
	}
	st.Memory = h.memory()
	st.CPUTemperatureC = h.cpuTemperature()
	return st
}

// diskStatus is the usage of the file system holding dir.
func diskStatus(dir string) *DiskStatus {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(dir, &fs); err != nil {
		return nil
	}
	unit := int64(fs.Frsize)
	if unit <= 0 {
		unit = int64(fs.Bsize)
	}
	if unit <= 0 {
		return nil
	}
	return &DiskStatus{FreeBytes: uint64(fs.Bavail) * uint64(unit), TotalBytes: uint64(fs.Blocks) * uint64(unit)}
}

// memory reads MemTotal and MemAvailable from meminfo.
func (h linuxHost) memory() *MemoryStatus {
	data, ok := readSmall(filepath.Join(h.proc, "meminfo"))
	if !ok {
		return nil
	}
	fields := map[string]uint64{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		name, rest, found := strings.Cut(sc.Text(), ":")
		if !found {
			continue
		}
		parts := strings.Fields(rest)
		if len(parts) != 2 || parts[1] != "kB" {
			continue
		}
		if n, err := strconv.ParseUint(parts[0], 10, 64); err == nil && n <= 1<<53 {
			fields[name] = n * 1024
		}
	}
	total, okTotal := fields["MemTotal"]
	avail, okAvail := fields["MemAvailable"]
	if !okTotal || !okAvail {
		return nil
	}
	return &MemoryStatus{TotalBytes: total, AvailableBytes: avail}
}

// cpuTemperature reads the CPU's sensor (hostsys.CPUTemperature), in
// degrees Celsius.
func (h linuxHost) cpuTemperature() *float64 {
	c, ok := hostsys.CPUTemperature(h.sys)
	if !ok {
		return nil
	}
	return &c
}

// readSmall reads at most maxProcFileBytes of a file.
func readSmall(path string) ([]byte, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxProcFileBytes))
	return data, err == nil
}
