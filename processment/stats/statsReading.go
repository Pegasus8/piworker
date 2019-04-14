package stats

import (
	"regexp"
	"strconv"
	"os/exec"
	"os"
	"io/ioutil"

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

	match := rgx.FindStringSubmatch(string(output))
	if match != nil {
		temp, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			return 0.0, err
		}
		return temp, nil
	}
	
	return 0.0, ErrBadTemperatureParse
}

func getRaspberryCPULoad() (cpuload string, err error) {
	filePath := "/proc/stat"
	rgx := regexp.MustCompile(
		`(?m)^cpu[^0-9]\s*([0-9]+)\s*([0-9]+)\s*([0-9]+)\s*([0-9]+)\s*([0-9]+)\s*([0-9]+)\s*([0-9]+)[\s0-9]*`,
	)

	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	content, err := ioutil.ReadAll(file)
	if err != nil {
		return "", err
	}

	match := rgx.FindStringSubmatch(string(content))
	if len(match) > 1 {
		values := make([]int64, 0)
		for index, m := range match {
			if index == 0 { continue } // Skip the line matched
			val, err := strconv.ParseInt(m, 10, 64)
			if err != nil {
				return "", err
			}
			values = append(values, val)
		}
		result := (values[3] * 100) / (values[0] + values[1] + values[2] + 
			values[3] + values[4] + values[5] + values[6])

		return string(result) + "%", nil
	} 
		
	return "", ErrBadCPULoadParse
}

func getRaspberryFreeStorage() (freestorage string, err error) {
	rgx := regexp.MustCompile(
		`(?m)^/dev/root\s+(\w+,?\w*)\s+(\w+,?\w*)\s+(\w+,?\w*).+$`,
	)

	cmd := exec.Command("df", "-h")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	match := rgx.FindStringSubmatch(string(output))
	if match != nil {
		freestorage := match[3]
		// totalstorage := match[1] -> can be used
		// usedstorage := match[2] -> can be used

		return freestorage, nil
	}

	return "", ErrBadFreeStorageParse
}

func getRaspberryRAMUsage() (ramusage string, err error) {
	rgx := regexp.MustCompile(
		`(?m)^Mem:\s+(\w+,?\w*)\s+(\w+,?\w*).+`,
	)

	cmd := exec.Command("free", "-h")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	match := rgx.FindStringSubmatch(string(output))
	if match != nil {
		ramusage := match[2]
		// totalmemory := match[1] -> can be used

		return ramusage, nil
	}

	return "", ErrBadRAMUsageParse
}