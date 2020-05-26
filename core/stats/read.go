package stats

import (
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/host"
	"github.com/shirou/gopsutil/mem"
)

var arch string

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
	Current.RaspberryStats.Host.BootTime = bt
	Current.RaspberryStats.Host.UpTime = ut
	
	var temperature float64
	// Architecture of a Raspberry Pi.
	if arch == "armv7l" {
		// If it's a Raspberry Pi then the slice of sensors should be only one.
		temperature = st[0].Temperature
	} else {
		// Not a Raspberry Pi, let's use our known key to find the temperature of the entire CPU.
		// Note: I'm not sure if this will work on all the machines but unless someone reports that is not
		// working I can't do anything else.
		for _, t := range st {
			if t.SensorKey == "coretemp_packageid0_input" {
				temperature = t.Temperature
			}
		}
	}
	Current.RaspberryStats.Host.Temperature = temperature

	return nil
}
