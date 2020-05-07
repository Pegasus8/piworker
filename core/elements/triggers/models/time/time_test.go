package time

import (
	"github.com/google/uuid"
	"time"
	"github.com/Pegasus8/piworker/core/data"
	"testing"

	test "github.com/Pegasus8/piworker/utilities/testing"

	"github.com/stretchr/testify/assert"
)

func TestByTime(t *testing.T) {
	taskID := uuid.New().String()
	assert := assert.New(t)

	test.CheckTFields(t, ByTime)

	args := [][]data.UserArg{
		// [0] -- Correct --
		// Problem: 		None.
		// Expected result: Should return no errors and a true result.
		[]data.UserArg{
			data.UserArg{
				ID: ByTime.Args[0].ID,
				Content: time.Now().Format("2006-01-02"),
			},
			data.UserArg{
				ID: ByTime.Args[1].ID,
				Content: time.Now().Format("15:04"),
			},
		},

		// [1] -- Incorrect --
		// Problem: 		The first arg ("Date") is incorrectly formatted.
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID: ByTime.Args[0].ID,
				Content: time.Now().Format("02/01/2006"), // Let's use a wrong format.
			},
			data.UserArg{
				ID: ByTime.Args[1].ID,
				Content: time.Now().Format("15:04"),
			},
		},

		// [2] -- Incorrect --
		// Problem: 		The second arg ("Hour") is incorrectly formatted.
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID: ByTime.Args[0].ID,
				Content: time.Now().Format("2006-01-02"),
			},
			data.UserArg{
				ID: ByTime.Args[1].ID,
				Content: time.Now().Format("15-04"), // Let's use a wrong format.
			},
		},

		// [3] -- Incorrect --
		// Problem: 		Both args are incorrectly formatted.
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID: ByTime.Args[0].ID,
				Content: time.Now().Format("2006/01/02"),
			},
			data.UserArg{
				ID: ByTime.Args[1].ID,
				Content: time.Now().Format("15m 04s"), // Let's use a wrong format.
			},
		},

		// [4] -- Incorrect --
		// Problem: 		ID of an arg (0) is empty.
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID: "", // Empty ID
				Content: time.Now().Format("2006/01/02"),
			},
			data.UserArg{
				ID: ByTime.Args[1].ID,
				Content: time.Now().Format("15:04"), // Let's use a wrong format.
			},
		},

		// [5] -- Incorrect --
		// Problem: 		ID of an arg (1) is incorrect.
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID: ByTime.Args[0].ID,
				Content: time.Now().Format("2006/01/02"),
			},
			data.UserArg{
				ID: ByTime.ID + "-5", // Non-existent ID
				Content: time.Now().Format("15m 04s"), // Let's use a wrong format.
			},
		},

		// [6] -- Incorrect --
		// Problem: 		There are only one argument (should be two).
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID: ByTime.Args[0].ID,
				Content: time.Now().Format("2006-01-02"),
			},
		},

		// [7] -- Incorrect --
		// Problem: 		Content of an argument empty.
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID: ByTime.Args[0].ID,
				Content: "",
			},
			data.UserArg{
				ID: ByTime.Args[1].ID, // Non-existent ID
				Content: time.Now().Format("15:04"), // Let's use a wrong format.
			},
		},
	}

	r, err := ByTime.Run(&args[0], taskID)
	assert.Equal(true, r, "the trigger must be executed correctly")
	if r {
		assert.NoError(err, "if the result of the trigger is true, there should be no errors")
	}

	for i, arg := range args[1:] {
		r, err := ByTime.Run(&arg, taskID)
		assert.Equal(false, r, "the trigger must return a false result if at least one argument is incorrect")
		assert.Errorf(err, "the usage of the argument %d must return an error", i)
	}
}