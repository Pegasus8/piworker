package types

/*
This package defines the standard types used by PiWorker.
The objective of this is basically have a centralized way of represent different types used on different places (like ChainedResult struct, user variables, etc).
*/

import (
	"regexp"
	"strconv"
)

// PWType represents the standard types used on different parts of PiWorker. The objective of this is have a centralized way of represent the content with a specific format for a better management of it.
type PWType string

const (
	// Any is the constant used to represent any type. Generally used on actions that doesn't
	// require a special type to work. For example user variables related actions.
	Any PWType = "any"
	// Text is the constant used to represent the content of type string that does not have a specific format.
	Text PWType = "text"
	// Int is the constant used to represent the content of type integer.
	Int PWType = "number"
	// Float is the constant used to reperesent the content of type float.
	Float PWType = "number-float"
	// Bool is the constant used to reperesent the content of type boolean.
	Bool PWType = "boolean"
	// Path is the constant used to reperesent the content of type path (example: "/home/pi/random/folder").
	Path PWType = "path"
	// JSON is the constant used to reperesent the content of type JSON (example: "{"foo": "bar"}").
	JSON PWType = "json"
	// URL is the constant used to reperesent the content of type URL. For example: "https://golang.org".
	URL PWType = "url"
	// Date is the constant used to represent the content with the format of a date. For example: "10/11/2020".
	Date PWType = "date"
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

// GetType identifies the type of the specified `value` (string). UNFINISHED.
func GetType(value string) PWType {
	if isInt, _ := IsInt(value); isInt {
		return Int
	} else if isFloat, _ := IsFloat(value); isFloat {
		return Float
	} else if isBool, _ := IsBool(value); isBool {
		return Bool
	} else if isPath, _ := IsPath(value); isPath {
		return Path
	} else {
		return Text
	}
}
