package stats

import (
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/host"
	"github.com/shirou/gopsutil/mem"
)

// UpdateRPiStats is a function update the statistics related with the host (usually a Raspberry Pi), on the variable `Current`.
func UpdateRPiStats() error {
	Current.Lock()
	defer Current.Unlock()

	// CPU stats
	cpuL, err := cpu.Percent(0, false)
	if err != nil {
		return err
	}
	Current.RaspberryStats.CPULoad = cpuL[0]

	// Storage stats
	d, err := disk.Usage("/")
	if err != nil {
		return err
	}
	Current.RaspberryStats.Storage.Total = d.Total
	Current.RaspberryStats.Storage.Free = d.Free
	Current.RaspberryStats.Storage.Used = d.Used
	Current.RaspberryStats.Storage.UsedPercent = d.UsedPercent

	// RAM stats
	vms, err := mem.VirtualMemory()
	if err != nil {
		return err
	}
	Current.RaspberryStats.RAM.Total = vms.Total
	Current.RaspberryStats.RAM.Available = vms.Available
	Current.RaspberryStats.RAM.Used = vms.Used

	// Host stats
	st, err := host.SensorsTemperatures()
	if err != nil {
		return err
	}
	bt, err := host.BootTime()
	if err != nil {
		return err
	}
	ut, err := host.Uptime()
	if err != nil {
		return err
	}
	Current.RaspberryStats.Host.Temperatures = st
	Current.RaspberryStats.Host.BootTime = bt
	Current.RaspberryStats.Host.UpTime = ut

	return nil
}
