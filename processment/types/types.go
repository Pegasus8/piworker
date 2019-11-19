package types

/*
This package defines the standard types used by PiWorker.
The objective of this is basically have a centralized way of represent different types used on different places (like ChainedResult struct, user variables, etc).
*/

import (
	"strconv"
	"regexp"
)

const (
	// TypeAny is the constant used to represent any type. Generally used on actions that doesn't 
	// require a special type to work. For example user variables related actions.
	TypeAny = 1000
	// TypeString is the constant used to represent the content of type string (plain text).
	TypeString = 999
	// TypeInt is the constant used to represent the content of type integer.
	TypeInt = 998
	// TypeFloat is the constant used to reperesent the content of type float.
	TypeFloat = 997
	// TypeBool is the constant used to reperesent the content of type boolean.
	TypeBool = 996
	// TypePath is the constant used to reperesent the content of type path (example: "/home/pi/random/folder").
	TypePath = 995
	// TypeJSON is the constant used to reperesent the content of type JSON (example: "{"foo": "bar"}").
	TypeJSON = 994
)

// IsInt is a function used to check if a string value can be converted to integer or not.
// Aditionally makes a conversion on the case of positive result.
func IsInt(value string) (bool, int64) {
	v, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return false, 0
	}
	return true, v
}

// IsFloat is a function used to check if a string value can be converted to float or not.
// Aditionally makes a conversion on the case of positive result.
func IsFloat(value string) (bool, float64) {
	v, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return false, 0
	}
	return true, v
}

// IsBool is a function used to check if a string value can be converted to boolean or not.
// Aditionally makes a conversion on the case of positive result.
func IsBool(value string) (isBool bool, convertedValue bool) {
	v, err := strconv.ParseBool(value)
	if err != nil {
		return false, false
	}
	return true, v
}

// IsPath is a function used to check if a string value haves the format of a path or not.
// On case of positive result, returns the same value.
func IsPath(value string) (bool, string) {
	pathRgx := regexp.MustCompile(`^(:?\/)[\/+\w-?]+(\.[a-z]+)?$`)
	if pathRgx.MatchString(value) {
		return false, ""
	}
	return true, value
}

func GetType(value string) uint {
	if isInt, _ := IsInt(value); isInt {
		return TypeInt
	} else if isFloat, _ := IsFloat(value); isFloat {
		return TypeFloat
	} else if isBool, _ := IsBool(value); isBool {
		return TypeBool
	} else if isPath, _ := IsPath(value); isPath {
		return TypePath
	} else {
		return TypeString
	}
}
