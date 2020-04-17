package temp

import (
	"errors"
	"strconv"

	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/elements/triggers/shared"
	"github.com/Pegasus8/piworker/core/types"

	"github.com/shirou/gopsutil/host"
)

// ID's
const (
	// Trigger
	triggerID = "T2"

	// Args
	arg1ID = triggerID + "-1"
)

// RaspberryTemperature - Trigger
var RaspberryTemperature = shared.Trigger{
	ID:          triggerID,
	Name:        "Raspberry's Temperature",
	Description: "",
	Run:         trigger,
	Args: []shared.Arg{
		shared.Arg{
			ID:   arg1ID,
			Name: "Expected Temperature",
			Description: "The expected temperature of the Raspberry Pi. Must be in" +
				" float format and without the 'ºC'. Example: 55.1.",
			// Content: "",
			ContentType: types.Float,
		},
	},
}

func trigger(args *[]data.UserArg, parentTaskID string) (result bool, err error) {

	// Expected temperature received
	var expectedTemp float64

	for _, arg := range *args {
		switch arg.ID {
		// Temperature arg
		case arg1ID:
			{
				expectedTemp, err = strconv.ParseFloat(arg.Content, 64)
				if err != nil {
					return false, err
				}
			}

		default:
			{
				return false, shared.ErrUnrecognizedArgID
			}
		}
	}

	st, err := host.SensorsTemperatures()
	if err != nil {
		return false, err
	}

	temperature := func() float64 {
		for _, t := range st {
			if t.SensorKey == "coretemp_packageid0_input" {
				return t.Temperature
			}
		}
		return 0.0
	}()

	if temperature == 0.0 {
		return false, errors.New("SensorKey incompatible with host")
	}

	if temperature == expectedTemp {
		return true, nil
	}

	return false, nil
}
