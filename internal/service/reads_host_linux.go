//go:build linux

package service

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
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

// cpuTemperature reads the CPU's thermal zone (by its type), or else the
// first zone with a plausible reading, in degrees Celsius.
func (h linuxHost) cpuTemperature() *float64 {
	zones, err := filepath.Glob(filepath.Join(h.sys, "class", "thermal", "thermal_zone*"))
	if err != nil || len(zones) == 0 {
		return nil
	}
	sort.Slice(zones, func(a, b int) bool { return zoneNumber(zones[a]) < zoneNumber(zones[b]) })
	var first *float64
	for _, zone := range zones {
		t := zoneTemperature(zone)
		if t == nil {
			continue
		}
		if isCPUZone(zone) {
			return t
		}
		if first == nil {
			first = t
		}
	}
	return first
}

func zoneNumber(path string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(path), "thermal_zone"))
	if err != nil {
		return 1 << 30
	}
	return n
}

func isCPUZone(zone string) bool {
	data, ok := readSmall(filepath.Join(zone, "type"))
	if !ok {
		return false
	}
	kind := strings.ToLower(strings.TrimSpace(string(data)))
	return strings.Contains(kind, "cpu") || kind == "x86_pkg_temp" || kind == "coretemp"
}

// zoneTemperature reads a zone's millidegrees; nil when unreadable or
// outside -50..150 °C.
func zoneTemperature(zone string) *float64 {
	data, ok := readSmall(filepath.Join(zone, "temp"))
	if !ok {
		return nil
	}
	milli, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || milli < -50_000 || milli > 150_000 {
		return nil
	}
	c := float64(milli) / 1000
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
