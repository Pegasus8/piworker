package typeconversion

import (
	"fmt"
)

// ConvertToString converts an interface to a string.
func ConvertToString(value interface{}) string {
	str := fmt.Sprintf("%v", value)
	return str
}

// ConvertToInt converts an interface to a integer.
func ConvertToInt(value interface{}) int {
	integer, ok := value.(int)
	if !ok {
		return 0
	}
	return integer
}

// ConvertToUint converts an interface to a unsigned integer.
func ConvertToUint(value interface{}) uint {
	unsignedInteger, ok := value.(uint)
	if !ok {
		return 0
	}
	return unsignedInteger
}

// ConvertToFloat converts an interface to a integer.
func ConvertToFloat(value interface{}) float64 {
	float, ok := value.(float64)
	if !ok {
		return 0.0
	}
	return float
}
