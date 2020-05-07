package temp

import (
	"fmt"
	"testing"

	"github.com/Pegasus8/piworker/core/data"
	test "github.com/Pegasus8/piworker/utilities/testing"

	"github.com/google/uuid"
	"github.com/shirou/gopsutil/host"
	"github.com/stretchr/testify/assert"
)

func TestRaspberryTemperature(t *testing.T) {
	taskID := uuid.New().String()
	assert := assert.New(t)

	test.CheckTFields(t, RaspberryTemperature)

	args := [][]data.UserArg{
		// [0] -- Correct --
		// Problem: 		None.
		// Expected result: Should return no errors and a true result.
		[]data.UserArg{
			data.UserArg{
				ID:      RaspberryTemperature.Args[0].ID,
				Content: fmt.Sprintf("%e", getTemperature(t)-20.0),
			},
		},

		// [1] -- Correct --
		// Problem: 		None.
		// Expected result: Should return no errors and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      RaspberryTemperature.Args[0].ID,
				Content: fmt.Sprintf("%e", getTemperature(t)+20.0),
			},
		},

		// [2] -- Incorrect --
		// Problem: 		The arg is incorrectly formatted.
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      RaspberryTemperature.Args[0].ID,
				Content: fmt.Sprintf("%e.0123", getTemperature(t)-20.0), // Let's use a wrong format.
			},
		},

		// [3] -- Incorrect --
		// Problem: 		ID of an arg (0) is empty.
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      "", // Empty ID
				Content: fmt.Sprintf("%e", getTemperature(t)-20.0),
			},
		},

		// [4] -- Incorrect --
		// Problem: 		ID of an arg is incorrect.
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      RaspberryTemperature.ID + "-5", // Non-existent ID
				Content: fmt.Sprintf("%e", getTemperature(t)-20.0),
			},
		},

		// [5] -- Incorrect --
		// Problem: 		There are no arguments (should be one).
		// Expected result: Should return an error and a false result.
		[]data.UserArg{},

		// [6] -- Incorrect --
		// Problem: 		Content of an argument empty.
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      RaspberryTemperature.Args[0].ID,
				Content: "", // Empty content
			},
		},
	}

	for i, arg := range args[:2] {
		r, err := RaspberryTemperature.Run(&arg, taskID)
		if i == 0 {
			assert.Equalf(true, r, "[arg %d] the trigger must be executed correctly", i)
		} else {
			assert.Equalf(false, r, "[arg %d] the trigger must be executed correctly", i)
		}
		assert.NoErrorf(err, "[arg %d] there should be no errors", i)
	}

	for i, arg := range args[2:] {
		r, err := RaspberryTemperature.Run(&arg, taskID)
		assert.Equalf(false, r, "[arg %d]the trigger must return a false result if at least one argument is incorrect", i)
		assert.Errorf(err, "[arg %d] an error must be returned", i)
	}
}

func getTemperature(t *testing.T) float64 {
	st, err := host.SensorsTemperatures()
	if err != nil {
		assert.FailNowf(t, "cannot get sensors temperatures: %s\n", err.Error())
	}

	for _, sensor := range st {
		if sensor.SensorKey == "coretemp_packageid0_input" {
			return sensor.Temperature
		}
	}

	assert.FailNow(t, "SensorKey incompatible")
	return 0.0
}
