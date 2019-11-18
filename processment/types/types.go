package types

/*
This package defines the standard types used by PiWorker.
The objective of this is basically have a centralized way of represent different types used on different places (like ChainedResult struct, user variables, etc).
*/

const (
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




