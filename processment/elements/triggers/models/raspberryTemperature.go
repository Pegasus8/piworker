package models

import (
	"regexp"
	"strconv"
	"os/exec"
	"log"

	"github.com/Pegasus8/piworker/processment/data"
	"github.com/Pegasus8/piworker/processment/elements/triggers"
	"github.com/Pegasus8/piworker/processment/stats"
)

// ID's
const (
	// Trigger
	raspberryTempID = "T3"

	// Args
	tempRaspberryTempArgID = "T3-1"
)

// RaspberryTemperature - Trigger 
var RaspberryTemperature = triggers.Trigger {
	ID: raspberryTempID,
	Name: "Raspberry's Temperature",
	Description: "",
	Run: raspberryTempTrigger,
	Args: []triggers.Arg {
		triggers.Arg {
			ID: tempRaspberryTempArgID,
			Name: "Expected Temperature",
			Description: "The expected temperature of the Raspberry Pi. Must be in" + 
				" float format and without the 'ºC'. Example: 55.1.",
			// Content: "",
			ContentType: "float",
		},
	},
}

func raspberryTempTrigger(args *[]data.UserArg) (result bool, err error) {

	// Expected temperature received
	var expectedTemp float64

	for _, arg := range *args {
		switch arg.ID {
			// Temperature arg
			case tempRaspberryTempArgID: {
				expectedTemp, err = strconv.ParseFloat(arg.Content, 64)
				if err != nil {
					return false, err
				}
			}

			default: {
				log.Printf("Unrecognized argument with the ID '%s' on the " + 
				"trigger RaspberryTemperature\n", arg.ID)
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

	if rgx.MatchString(string(output)){
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