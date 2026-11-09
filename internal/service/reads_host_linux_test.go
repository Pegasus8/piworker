//go:build linux

package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLinux lays out the /proc and /sys files the host probe reads.
func fakeLinux(t *testing.T, files map[string]string) linuxHost {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return linuxHost{proc: filepath.Join(root, "proc"), sys: filepath.Join(root, "sys")}
}

const meminfo = `MemTotal:        3884572 kB
MemFree:          912340 kB
MemAvailable:    2950012 kB
Buffers:          101236 kB
`

func TestLinuxHostProbe(t *testing.T) {
	h := fakeLinux(t, map[string]string{
		"proc/meminfo":                         meminfo,
		"sys/class/thermal/thermal_zone0/type": "acpitz\n",
		"sys/class/thermal/thermal_zone0/temp": "27800\n",
		"sys/class/thermal/thermal_zone1/type": "cpu-thermal\n",
		"sys/class/thermal/thermal_zone1/temp": "51234\n",
	})
	st := h.probe(t.TempDir())
	require.NotNil(t, st.Memory)
	assert.Equal(t, MemoryStatus{TotalBytes: 3884572 * 1024, AvailableBytes: 2950012 * 1024}, *st.Memory)
	require.NotNil(t, st.CPUTemperatureC)
	assert.InDelta(t, 51.234, *st.CPUTemperatureC, 1e-9, "the CPU's zone wins over others")
	require.NotNil(t, st.Disk)
	assert.Positive(t, st.Disk.TotalBytes)
	assert.LessOrEqual(t, st.Disk.FreeBytes, st.Disk.TotalBytes)
}

func TestLinuxHostProbeFallsBackToTheFirstZone(t *testing.T) {
	h := fakeLinux(t, map[string]string{
		"sys/class/thermal/thermal_zone0/type": "soc-dummy\n",
		"sys/class/thermal/thermal_zone0/temp": "45000\n",
	})
	st := h.probe("")
	require.NotNil(t, st.CPUTemperatureC)
	assert.InDelta(t, 45.0, *st.CPUTemperatureC, 1e-9)
}

// What cannot be read, or reads as nonsense, is omitted.
func TestLinuxHostProbeUnknown(t *testing.T) {
	h := fakeLinux(t, map[string]string{
		"proc/meminfo":                         "MemTotal: 1000 kB\n", // no MemAvailable (kernels before 3.14)
		"sys/class/thermal/thermal_zone0/type": "cpu-thermal\n",
		"sys/class/thermal/thermal_zone0/temp": "not a number\n",
		"sys/class/thermal/thermal_zone1/type": "cpu-thermal\n",
		"sys/class/thermal/thermal_zone1/temp": "999999999\n", // out of range
	})
	st := h.probe(filepath.Join(t.TempDir(), "missing"))
	assert.Nil(t, st.Memory)
	assert.Nil(t, st.CPUTemperatureC)
	assert.Nil(t, st.Disk, "an unreadable data directory has no disk figures")
	assert.Equal(t, HostStatus{}, fakeLinux(t, nil).probe(""), "nothing to read: nothing reported")
}
