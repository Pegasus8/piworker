package everyxtime

import (
	"time"
	"testing"

	"github.com/Pegasus8/piworker/core/data"
	test "github.com/Pegasus8/piworker/utilities/testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestEveryXTime(t *testing.T) {
	taskID := uuid.New().String()
	assert := assert.New(t)

	test.CheckTFields(t, EveryXTime)

	args := [][]data.UserArg{
		// [0] -- Correct --
		// Problem: 		None.
		// Expected result: Should return no errors and a true result.
		[]data.UserArg{
			data.UserArg{
				ID:      EveryXTime.Args[0].ID,
				Content: "5s",
			},
		},

		// [1] -- Correct --
		// Problem: 		None.
		// Expected result: Should return no errors and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      EveryXTime.Args[0].ID,
				Content: "5s",
			},
		},

		// [2] -- Incorrect --
		// Problem: 		The arg is incorrectly formatted.
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      EveryXTime.Args[0].ID,
				Content: "5something", // Let's use a wrong format.
			},
		},

		// [3] -- Incorrect --
		// Problem: 		ID of an arg (0) is empty.
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      "", // Empty ID
				Content: "5s",
			},
		},

		// [4] -- Incorrect --
		// Problem: 		ID of an arg is incorrect.
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      EveryXTime.ID + "-5", // Non-existent ID
				Content: "5s",
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
				ID:      EveryXTime.Args[0].ID,
				Content: "", // Empty content
			},
		},
	}

	// Set the next execution to the current time to activate the trigger.
	nextExecution[taskID] = time.Now()
	r, err := EveryXTime.Run(&args[0], taskID)
	assert.Equal(true, r, "the trigger must be executed correctly")
	assert.NoError(err, "there should be no errors")

	r, err = EveryXTime.Run(&args[1], taskID)
	assert.Equal(false, r, "the trigger must be executed correctly")
	assert.NoError(err, "there should be no errors")

	for i, arg := range args[2:] {
		r, err := EveryXTime.Run(&arg, taskID)
		assert.Equalf(false, r, "[arg %d]the trigger must return a false result if at least one argument is incorrect", i)
		assert.Errorf(err, "[arg %d] an error must be returned", i)
	}
}
