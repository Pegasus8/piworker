package hostsys

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSys lays out files below a temporary /sys root.
func fakeSys(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return root
}

func requireTemp(t *testing.T, sys string, want float64, msg string) {
	t.Helper()
	got, ok := CPUTemperature(sys)
	require.True(t, ok, msg)
	assert.InDelta(t, want, got, 1e-9, msg)
}

// Raspberry Pi: one zone, cpu-thermal.
func TestCPUTemperatureRaspberryPi(t *testing.T) {
	sys := fakeSys(t, map[string]string{
		"class/thermal/thermal_zone0/type": "cpu-thermal\n",
		"class/thermal/thermal_zone0/temp": "48312\n",
	})
	requireTemp(t, sys, 48.312, "the Pi's only zone is its CPU")
}

// Intel PC: zone0 is the ACPI zone (a board sensor, often a fixed 27.8 °C);
// the package sensor is in a later zone.
func TestCPUTemperatureIntelPrefersThePackageZone(t *testing.T) {
	sys := fakeSys(t, map[string]string{
		"class/thermal/thermal_zone0/type":  "acpitz\n",
		"class/thermal/thermal_zone0/temp":  "27800\n",
		"class/thermal/thermal_zone1/type":  "INT3400 Thermal\n",
		"class/thermal/thermal_zone1/temp":  "20000\n",
		"class/thermal/thermal_zone10/type": "x86_pkg_temp\n",
		"class/thermal/thermal_zone10/temp": "61000\n",
		"class/thermal/thermal_zone2/type":  "pch_cannonlake\n",
		"class/thermal/thermal_zone2/temp":  "45000\n",
	})
	requireTemp(t, sys, 61.0, "x86_pkg_temp wins over acpitz in zone0")
}

// AMD PC: no CPU thermal zone at all; the CPU sensor is the k10temp hwmon.
func TestCPUTemperatureAMDUsesHwmon(t *testing.T) {
	sys := fakeSys(t, map[string]string{
		"class/thermal/thermal_zone0/type": "acpitz\n",
		"class/thermal/thermal_zone0/temp": "16800\n",
		"class/hwmon/hwmon0/name":          "nvme\n",
		"class/hwmon/hwmon0/temp1_input":   "38850\n",
		"class/hwmon/hwmon1/name":          "k10temp\n",
		"class/hwmon/hwmon1/temp1_input":   "55125\n",
	})
	requireTemp(t, sys, 55.125, "k10temp wins over the ACPI zone and other hwmon chips")
}

// Intel without the x86_pkg_temp zone (module not loaded): coretemp hwmon.
func TestCPUTemperatureCoretempHwmon(t *testing.T) {
	sys := fakeSys(t, map[string]string{
		"class/hwmon/hwmon3/name":        "coretemp\n",
		"class/hwmon/hwmon3/temp1_input": "52000\n",
		"class/hwmon/hwmon3/temp2_input": "50000\n",
	})
	requireTemp(t, sys, 52.0, "temp1 is the package sensor")
}

// A CPU zone wins over a CPU hwmon chip: both describe the CPU, and zones are
// what the Pi and most ARM boards expose.
func TestCPUTemperatureZoneBeforeHwmon(t *testing.T) {
	sys := fakeSys(t, map[string]string{
		"class/thermal/thermal_zone0/type": "x86_pkg_temp\n",
		"class/thermal/thermal_zone0/temp": "60000\n",
		"class/hwmon/hwmon0/name":          "coretemp\n",
		"class/hwmon/hwmon0/temp1_input":   "59000\n",
	})
	requireTemp(t, sys, 60.0, "the zone is read first")
}

// No sensor identified as the CPU's: the first plausible zone, as before.
func TestCPUTemperatureFallsBackToTheFirstZone(t *testing.T) {
	sys := fakeSys(t, map[string]string{
		"class/thermal/thermal_zone1/type": "soc-dummy\n",
		"class/thermal/thermal_zone1/temp": "47000\n",
		"class/thermal/thermal_zone0/type": "soc-dummy\n",
		"class/thermal/thermal_zone0/temp": "45000\n",
	})
	requireTemp(t, sys, 45.0, "zones are ordered by number")
}

// What cannot be read, or reads as nonsense, is skipped.
func TestCPUTemperatureUnknown(t *testing.T) {
	sys := fakeSys(t, map[string]string{
		"class/thermal/thermal_zone0/type": "cpu-thermal\n",
		"class/thermal/thermal_zone0/temp": "not a number\n",
		"class/thermal/thermal_zone1/type": "cpu-thermal\n",
		"class/thermal/thermal_zone1/temp": "999999999\n", // out of range
		"class/hwmon/hwmon0/name":          "k10temp\n",
		"class/hwmon/hwmon0/temp1_input":   "-90000\n", // out of range
	})
	_, ok := CPUTemperature(sys)
	assert.False(t, ok)

	_, ok = CPUTemperature(fakeSys(t, nil))
	assert.False(t, ok, "nothing to read: nothing reported")
}
