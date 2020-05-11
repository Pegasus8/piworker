package testing

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/Pegasus8/piworker/core/elements/actions/shared"

	"github.com/stretchr/testify/assert"
)

// CheckAFields checks if some of the fields of type string is empty. Must be used **only** to test actions.
func CheckAFields(t *testing.T, action shared.Action) {
	assert := assert.New(t)

	// Check if the format of the action's ID is "An", where "n" is an unsigned integer.
	assert.Regexp(regexp.MustCompile("^A[0-9]{1,2}$"), action.ID, "the format of the ID must be 'An', where n is the number of the action")
	assert.NotEqual("", action.Name, "the Name field must not be empty")
	assert.NotEqual("", action.Description, "the Description field must not be empty")
	assert.NotEqual("", action.ReturnedChainResultDescription, "the action should have a description of the chained result returned")
	assert.NotEqual("", action.ReturnedChainResultType, "the ReturnedChainResultType field must not be empty")

	for i, arg := range action.Args {
		// Check if the format of each ID is correct (format: "An-an", where "an" is the number of the argument)
		// Also, the args must be ordered, so "an" must be i+1.
		rgx := fmt.Sprintf("^A[0-9]{1,2}-%d$", i+1)
		assert.Regexpf(regexp.MustCompile(rgx), arg.ID, "format of the ID of the arg %d is incorrect", i)
		assert.NotEqualf("", arg.Name, "the Name field of the arg %d must not be empty", i)
		assert.NotEqualf("", arg.Description, "the Description field of the arg %d must not be empty", i)
		assert.NotEqualf("", arg.ContentType, "the ContentType field of the arg %d must not be empty", i)
	}
}
