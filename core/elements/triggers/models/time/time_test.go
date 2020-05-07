package time

import (
	"testing"
	"time"

	"github.com/Pegasus8/piworker/core/data"
	test "github.com/Pegasus8/piworker/utilities/testing"

	"github.com/google/uuid"
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
				ID:      ByTime.Args[0].ID,
				Content: time.Now().Format("2006-01-02"),
			},
			data.UserArg{
				ID:      ByTime.Args[1].ID,
				Content: time.Now().Format("15:04"),
			},
		},

		// [1] -- Correct --
		// Problem: 		None.
		// Expected result: Should return no errors and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      ByTime.Args[0].ID,
				Content: time.Now().Add(time.Hour * 24).Format("2006-01-02"),
			},
			data.UserArg{
				ID:      ByTime.Args[1].ID,
				Content: time.Now().Format("15:04"),
			},
		},

		// [2] -- Correct --
		// Problem: 		None.
		// Expected result: Should return no errors and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      ByTime.Args[0].ID,
				Content: time.Now().Format("2006-01-02"),
			},
			data.UserArg{
				ID:      ByTime.Args[1].ID,
				Content: time.Now().Add(time.Minute * 2).Format("15:04"),
			},
		},

		// [3] -- Correct --
		// Problem: 		None.
		// Expected result: Should return no errors and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      ByTime.Args[0].ID,
				Content: time.Now().Add(time.Hour * 24).Format("2006-01-02"),
			},
			data.UserArg{
				ID:      ByTime.Args[1].ID,
				Content: time.Now().Add(time.Minute * 2).Format("15:04"),
			},
		},

		// [4] -- Incorrect --
		// Problem: 		The first arg ("Date") is incorrectly formatted.
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      ByTime.Args[0].ID,
				Content: time.Now().Format("02/01/2006"), // Let's use a wrong format.
			},
			data.UserArg{
				ID:      ByTime.Args[1].ID,
				Content: time.Now().Format("15:04"),
			},
		},

		// [5] -- Incorrect --
		// Problem: 		The second arg ("Hour") is incorrectly formatted.
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      ByTime.Args[0].ID,
				Content: time.Now().Format("2006-01-02"),
			},
			data.UserArg{
				ID:      ByTime.Args[1].ID,
				Content: time.Now().Format("15-04"), // Let's use a wrong format.
			},
		},

		// [6] -- Incorrect --
		// Problem: 		Both args are incorrectly formatted.
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      ByTime.Args[0].ID,
				Content: time.Now().Format("2006/01/02"), // Wrong format here
			},
			data.UserArg{
				ID:      ByTime.Args[1].ID,
				Content: time.Now().Format("15m 04s"), // Wrong format here
			},
		},

		// [7] -- Incorrect --
		// Problem: 		ID of an arg (0) is empty.
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      "", // Empty ID
				Content: time.Now().Format("2006-01-02"),
			},
			data.UserArg{
				ID:      ByTime.Args[1].ID,
				Content: time.Now().Format("15:04"),
			},
		},

		// [8] -- Incorrect --
		// Problem: 		ID of an arg (1) is incorrect.
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      ByTime.Args[0].ID,
				Content: time.Now().Format("2006-01-02"),
			},
			data.UserArg{
				ID:      ByTime.ID + "-5",             // Non-existent ID
				Content: time.Now().Format("15:04"),
			},
		},

		// [9] -- Incorrect --
		// Problem: 		There are only one argument (should be two).
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      ByTime.Args[0].ID,
				Content: time.Now().Format("2006-01-02"),
			},
		},

		// [10] -- Incorrect --
		// Problem: 		Content of an argument empty.
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      ByTime.Args[0].ID,
				Content: "", // Empty content
			},
			data.UserArg{
				ID:      ByTime.Args[1].ID,
				Content: time.Now().Format("15:04"),
			},
		},
	}

	for i, arg := range args[:4] {
		r, err := ByTime.Run(&arg, taskID)
		if i == 0 {
			assert.Equalf(true, r, "[arg %d] the trigger must be executed correctly", i)
		} else {
			assert.Equalf(false, r, "[arg %d] the trigger must be executed correctly", i)
		}
		assert.NoErrorf(err, "[arg %d] there should be no errors", i)
	}

	for i, arg := range args[4:] {
		r, err := ByTime.Run(&arg, taskID)
		assert.Equalf(false, r, "[arg %d]the trigger must return a false result if at least one argument is incorrect", i)
		assert.Errorf(err, "[arg %d] an error must be returned", i)
	}
}
