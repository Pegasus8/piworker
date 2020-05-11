package testing

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/Pegasus8/piworker/core/elements/triggers/shared"

	"github.com/stretchr/testify/assert"
)

// CheckTFields checks if some of the fields of type string is empty.
func CheckTFields(t *testing.T, trigger shared.Trigger) {
	assert := assert.New(t)

	// Check if the format of the trigger's ID is "Tn", where "n" is an unsigned integer.
	assert.Regexp(regexp.MustCompile("^T[0-9]{1,2}$"), trigger.ID, "the format of the ID must be 'Tn', where n is the number of the trigger")
	assert.NotEqual("", trigger.Name, "the Name field must not be empty")
	assert.NotEqual("", trigger.Description, "the Description field must not be empty")

	for i, arg := range trigger.Args {
		// Check if the format of each ID is correct (format: "Tn-an", where "an" is the number of the argument)
		// Also, the args must be ordered, so "an" must be i+1.
		rgx := fmt.Sprintf("^T[0-9]{1,2}-%d$", i+1)
		assert.Regexpf(regexp.MustCompile(rgx), arg.ID, "format of the ID of the arg %d is incorrect", i)
		assert.NotEqualf("", arg.Name, "the Name field of the arg %d must not be empty", i)
		assert.NotEqualf("", arg.Description, "the Description field of the arg %d must not be empty", i)
		assert.NotEqualf("", arg.ContentType, "the ContentType field of the arg %d must not be empty", i)
	}
}
