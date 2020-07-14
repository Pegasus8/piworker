package types

import (
	"testing"

	assert2 "github.com/stretchr/testify/assert"
)

func TestCompatWith(t *testing.T) {
	assert := assert2.New(t)

	assert.True(Bool.CompatWith(Int), "the method should return true if the type used to compare "+
		"is compatible")
	assert.False(Path.CompatWith(Int), "the method should return false if the type used to compare "+
		"is not compatible")
	assert.Panics(func() { PWType("random-type").CompatWith(Bool) }, "if a non-existent type is used "+
		"as the method's parent the execution should panic")
	assert.False(JSON.CompatWith(PWType("random-type")), "the method should return false if the type"+
		" used to compare does not exist")
}

func TestCompatList(t *testing.T) {
	assert := assert2.New(t)

	// Keep this slice ALWAYS UP TO DATE.
	typesList := []PWType{
		Any,
		Text,
		Int,
		Float,
		Bool,
		Path,
		JSON,
		URL,
		Date,
		Time,
	}

	returnedTypes := CompatList()

	for i, t := range typesList {
		if t == "" {
			assert.FailNowf("the content of a type constant can't be empty", "the content of the type's constant 'typesList[%d]' is empty", i)
		}

		var exists bool
		for t2 := range returnedTypes {
			if t2 == t {
				exists = true
				break
			}
		}

		assert.Truef(exists, "the type '%s' isn't in the returned map of compatibilities", t)
	}
}
