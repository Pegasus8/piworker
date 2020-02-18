package models

import (
	"log"
	"os/exec"
	"regexp"
	"strconv"

	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/elements/triggers"
	"github.com/Pegasus8/piworker/core/stats"
	"github.com/Pegasus8/piworker/core/types"
)

// ID's
const (
	// Trigger
	raspberryTempID = "T3"

	// Args
	tempRaspberryTempArgID = "T3-1"
)

// RaspberryTemperature - Trigger
var RaspberryTemperature = triggers.Trigger{
	ID:          raspberryTempID,
	Name:        "Raspberry's Temperature",
	Description: "",
	Run:         raspberryTempTrigger,
	Args: []triggers.Arg{
		triggers.Arg{
			ID:   tempRaspberryTempArgID,
			Name: "Expected Temperature",
			Description: "The expected temperature of the Raspberry Pi. Must be in" +
				" float format and without the 'ºC'. Example: 55.1.",
			// Content: "",
			ContentType: types.Float,
		},
	},
}

func raspberryTempTrigger(args *[]data.UserArg, parentTaskID string) (result bool, err error) {

	// Expected temperature received
	var expectedTemp float64

	for _, arg := range *args {
		switch arg.ID {
		// Temperature arg
		case tempRaspberryTempArgID:
			{
				expectedTemp, err = strconv.ParseFloat(arg.Content, 64)
				if err != nil {
					return false, err
				}
			}

		default:
			{
				log.Printf("[%s] Unrecognized argument with the ID '%s' on the "+
					"trigger RaspberryTemperature\n", parentTaskID, arg.ID)
				return false, ErrUnrecognizedArgID
			}
		}
	}
	rgx := regexp.MustCompile(`(?m)^\w+=([0-9]+\.[0-9]).+$`)

	cmd := exec.Command("vcgencmd", "measure_temp")
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}

	if rgx.MatchString(string(output)) {
		match := rgx.FindStringSubmatch(string(output))
		if match != nil {
			temp, err := strconv.ParseFloat(match[1], 64)
			if err != nil {
				return false, err
			}

			if expectedTemp == temp {
				return true, nil
			}
			return false, nil
		}
	}

	return false, stats.ErrBadTemperatureParse
}
