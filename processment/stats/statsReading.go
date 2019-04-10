package stats

import (
	"regexp"
	"strconv"
	"os/exec"

	"github.com/Pegasus8/piworker/processment/data"
)

func GetStatistics() (statistics *Statistic, err error) {
	tasks, err := data.ReadData()
	if err != nil {
		return nil, err
	}

	activeTasks := len(*tasks.GetActiveTasks())
	inactiveTasks := len(*tasks.GetInactiveTasks())
	completedTasks := len(*tasks.GetCompletedTasks())
	onExecutionTasks := len(*tasks.OnExecutionTasks())

	// Raspberry
	rTemperature, err := getRaspberryTemperature()
	if err != nil {
		return nil, err
	}
	rCPULoad, err := getRaspberryCPULoad()
	if err != nil {
		return nil, err
	}
	rFreeStorage, err := getRaspberryFreeStorage()
	if err != nil {
		return nil, err
	}
	rFilesCreated, err := getRaspberryFilesCreated()
	if err != nil {
		return nil, err
	}
	rRAMUsage,err := getRaspberryRAMUsage()
	if err != nil {
		return nil, err
	}

	return &Statistic {
		ActiveTasks: activeTasks,
		InactiveTasks: inactiveTasks,
		OnExecutionTasks: onExecutionTasks,
		CompletedTasks: completedTasks,
		AverageExecutionTime: 0.0, //TODO
		OperatingTime: 0, //TODO
		BackupLoopState: data.BackupLoopState,
		
		RaspberryStats: RaspberryStats {
			Temperature: rTemperature,
			CPULoad: rCPULoad,
			FreeStorage: rFreeStorage,
			FilesCreated: rFilesCreated,
			RAMUsage: rRAMUsage,
		},
	}, nil
}

func getRaspberryTemperature() (temperature float64, err error) {
	rgx := regexp.MustCompile(`(?m)^\w+=([0-9]+\.[0-9]).+$`)
	
	cmd := exec.Command("vcgencmd", "measure_temp")
	output, err := cmd.Output()
	if err != nil {
		return 0.0, err
	}

	match := rgx.Find([]byte(output))
	if match != nil {
		temp, err := strconv.ParseFloat(string(match), 64)
		if err != nil {
			return 0.0, err
		}
		return temp, nil
	}

	return 0.0, ErrBadTemperatureParse
}