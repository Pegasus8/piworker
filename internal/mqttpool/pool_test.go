package mqttpool

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFiltersOverlap(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"z/a", "z/a", true},
		{"z/a", "z/b", false},
		{"z/a", "z/a/availability", false},
		{"z/#", "z/a/b", true},
		{"z/#", "z", true}, // # also matches the parent level
		{"z", "z/#", true},
		{"z/+", "z/a", true},
		{"z/+", "z/a/b", false},
		{"z/+/set", "z/a/get", false},
		{"+/a", "b/+", true},
		{"#", "anything/at/all", true},
		{"#", "$SYS/broker/uptime", false}, // wildcards never match $ topics at the first level
		{"+/uptime", "$SYS/uptime", false},
		{"$SYS/#", "$SYS/broker", true},
	} {
		assert.Equal(t, c.want, FiltersOverlap(c.a, c.b), "%s vs %s", c.a, c.b)
		assert.Equal(t, c.want, FiltersOverlap(c.b, c.a), "%s vs %s", c.b, c.a)
	}
}
