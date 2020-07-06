package types

var c = map[PWType][]PWType{
	Any: {
		Text,
		Time,
		Int,
		Float,
		Bool,
		Path,
		JSON,
		URL,
		Date,
		Time,
	},

	Text: {
		Time,
		Int,
		Float,
		Bool,
		Path,
		JSON,
		URL,
		Date,
		Time,
	},

	Int: {
		Any,
		Text,
	},

	Float: {
		Any,
		Text,
		Int,
	},

	Bool: {
		Any,
		Text,
		Int,
		Float,
	},

	Path: {
		Any,
		Text,
	},

	JSON: {
		Any,
		Text,
	},

	URL: {
		Any,
		Text,
	},

	Date: {
		Any,
		Text,
	},

	Time: {
		Any,
		Text,
	},
}

// CompatWith checks if a type is compatible with other type (`targetType`).
func (t PWType) CompatWith(typeTarget PWType) bool {
	for _, v := range c[t] {
		if v == typeTarget {
			return true
		}
	}

	return false
}

// CompatList returns a list of types that is compatible with the given type `t`.
func CompatList(t PWType) []PWType {
	return c[t]
}
