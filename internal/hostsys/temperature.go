// Package hostsys reads host figures from Linux's /sys, on every architecture
// PiWorker ships for: the Raspberry Pi and other ARM boards expose the CPU as
// a thermal zone, x86_64 PCs as a thermal zone (Intel's x86_pkg_temp) or only
// as a hwmon chip (Intel coretemp, AMD k10temp).
package hostsys

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// maxFileBytes bounds what is read of one sysfs file.
const maxFileBytes = 4 << 10

// cpuHwmonChips are the hwmon drivers whose temp1 is the CPU (package, Tctl
// or Tdie) temperature.
var cpuHwmonChips = map[string]bool{
	"coretemp":    true, // Intel
	"k10temp":     true, // AMD
	"zenpower":    true, // AMD, out-of-tree driver
	"cpu_thermal": true, // ARM boards that mirror their zone as hwmon
	"cpu-thermal": true,
}

// CPUTemperature returns the CPU temperature in degrees Celsius, reading the
// sysfs tree mounted at sys (normally "/sys"). It prefers, in order: a thermal
// zone whose type names the CPU, a hwmon chip that is the CPU's sensor, and
// then the lowest-numbered zone with a plausible reading. ok is false when no
// plausible reading exists.
func CPUTemperature(sys string) (celsius float64, ok bool) {
	zones := numbered(filepath.Join(sys, "class", "thermal", "thermal_zone*"), "thermal_zone")
	var first *float64
	for _, zone := range zones {
		t, ok := milliCelsius(filepath.Join(zone, "temp"))
		if !ok {
			continue
		}
		if isCPUZone(zone) {
			return t, true
		}
		if first == nil {
			first = &t
		}
	}
	for _, chip := range numbered(filepath.Join(sys, "class", "hwmon", "hwmon*"), "hwmon") {
		name, ok := readSmall(filepath.Join(chip, "name"))
		if !ok || !cpuHwmonChips[strings.ToLower(strings.TrimSpace(name))] {
			continue
		}
		if t, ok := milliCelsius(filepath.Join(chip, "temp1_input")); ok {
			return t, true
		}
	}
	if first != nil {
		return *first, true
	}
	return 0, false
}

// numbered globs pattern and sorts the matches by the number after prefix in
// their base names (thermal_zone2 before thermal_zone10).
func numbered(pattern, prefix string) []string {
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	number := func(path string) int {
		n, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(path), prefix))
		if err != nil {
			return 1 << 30
		}
		return n
	}
	sort.SliceStable(paths, func(a, b int) bool { return number(paths[a]) < number(paths[b]) })
	return paths
}

func isCPUZone(zone string) bool {
	kind, ok := readSmall(filepath.Join(zone, "type"))
	if !ok {
		return false
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	return strings.Contains(kind, "cpu") || kind == "x86_pkg_temp" || kind == "coretemp"
}

// milliCelsius reads a sysfs millidegree value; ok is false when it is
// unreadable or outside -50..150 °C.
func milliCelsius(path string) (float64, bool) {
	data, ok := readSmall(path)
	if !ok {
		return 0, false
	}
	milli, err := strconv.ParseInt(strings.TrimSpace(data), 10, 64)
	if err != nil || milli < -50_000 || milli > 150_000 {
		return 0, false
	}
	return float64(milli) / 1000, true
}

// readSmall reads at most maxFileBytes of a file.
func readSmall(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes))
	return string(data), err == nil
}
