package builtin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// useFakeSysfs points the sysmetric trigger at a temporary /sys tree.
func useFakeSysfs(t *testing.T, files map[string]string) {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	prev := sysfsRoot
	sysfsRoot = root
	t.Cleanup(func() { sysfsRoot = prev })
}

// On an x86_64 PC thermal_zone0 is usually the ACPI board zone; the trigger
// must watch the CPU's sensor instead.
func TestSysMetricTemperatureReadsTheCPUOnX86(t *testing.T) {
	useFakeSysfs(t, map[string]string{
		"class/thermal/thermal_zone0/type": "acpitz\n",
		"class/thermal/thermal_zone0/temp": "27800\n",
		"class/thermal/thermal_zone1/type": "x86_pkg_temp\n",
		"class/thermal/thermal_zone1/temp": "71000\n",
	})
	v, err := readSystemMetric("temperature")
	require.NoError(t, err)
	assert.InDelta(t, 71.0, v, 1e-9)
}

// AMD PCs expose the CPU only as the k10temp hwmon chip.
func TestSysMetricTemperatureReadsAMDHwmon(t *testing.T) {
	useFakeSysfs(t, map[string]string{
		"class/hwmon/hwmon2/name":        "k10temp\n",
		"class/hwmon/hwmon2/temp1_input": "63500\n",
	})
	v, err := readSystemMetric("temperature")
	require.NoError(t, err)
	assert.InDelta(t, 63.5, v, 1e-9)
}

func TestSysMetricTemperatureWithoutSensor(t *testing.T) {
	useFakeSysfs(t, nil)
	_, err := readSystemMetric("temperature")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no CPU temperature sensor")
}
