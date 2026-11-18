package types

import (
	"testing"
)

// TestCloneDeepCopiesTypedCollections verifies that Clone does not share
// mutable backing storage for the typed maps/slices nodes commonly produce
// (headers, string lists, raw bytes, numeric series, lists of records).
// Mutating the clone must never be visible through the original.
func TestCloneDeepCopiesTypedCollections(t *testing.T) {
	type custom struct{ N int }

	orig := NewMessage(map[string]interface{}{
		"headers": map[string]string{"a": "1"},
		"rows":    []map[string]interface{}{{"x": 1}},
		"tags":    []string{"t1"},
		"raw":     []byte("ab"),
		"series":  []float64{1.5},
		"ints":    []int{1},
		"nested":  map[string][]string{"k": {"v"}},
		"anymap":  map[string]any{"deep": []map[string]any{{"y": "z"}}},
		"structs": []custom{{N: 1}},
		"intmap":  map[int]string{1: "one"},
	}, DataTypeObject)
	orig.Meta["headers"] = map[string]string{"h": "v"}
	orig.Meta["list"] = []string{"m"}

	c := orig.Clone()
	p := c.Payload.(map[string]interface{})

	p["headers"].(map[string]string)["a"] = "changed"
	p["rows"].([]map[string]interface{})[0]["x"] = 99
	p["tags"].([]string)[0] = "changed"
	p["raw"].([]byte)[0] = 'z'
	p["series"].([]float64)[0] = 9
	p["ints"].([]int)[0] = 9
	p["nested"].(map[string][]string)["k"][0] = "changed"
	p["anymap"].(map[string]any)["deep"].([]map[string]any)[0]["y"] = "changed"
	p["structs"].([]custom)[0].N = 9
	p["intmap"].(map[int]string)[1] = "changed"
	c.Meta["headers"].(map[string]string)["h"] = "changed"
	c.Meta["list"].([]string)[0] = "changed"

	op := orig.Payload.(map[string]interface{})
	checks := []struct {
		name      string
		got, want interface{}
	}{
		{"map[string]string", op["headers"].(map[string]string)["a"], "1"},
		{"[]map[string]interface{}", op["rows"].([]map[string]interface{})[0]["x"], 1},
		{"[]string", op["tags"].([]string)[0], "t1"},
		{"[]byte", op["raw"].([]byte)[0], byte('a')},
		{"[]float64", op["series"].([]float64)[0], 1.5},
		{"[]int", op["ints"].([]int)[0], 1},
		{"map[string][]string", op["nested"].(map[string][]string)["k"][0], "v"},
		{"nested map[string]any", op["anymap"].(map[string]any)["deep"].([]map[string]any)[0]["y"], "z"},
		{"[]struct", op["structs"].([]custom)[0].N, 1},
		{"map[int]string", op["intmap"].(map[int]string)[1], "one"},
		{"meta map[string]string", orig.Meta["headers"].(map[string]string)["h"], "v"},
		{"meta []string", orig.Meta["list"].([]string)[0], "m"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: original mutated through clone: got %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestCloneKeepsNilsAndScalars(t *testing.T) {
	var nilMap map[string]string
	var nilSlice []string
	m := NewMessage(map[string]interface{}{"m": nilMap, "s": nilSlice, "n": 1, "str": "x"}, DataTypeObject)
	c := m.Clone().Payload.(map[string]interface{})
	if c["m"].(map[string]string) != nil {
		t.Error("nil map should stay nil")
	}
	if c["s"].([]string) != nil {
		t.Error("nil slice should stay nil")
	}
	if c["n"] != 1 || c["str"] != "x" {
		t.Error("scalars must be copied as-is")
	}
	if NewMessage(nil, DataTypeAny).Clone().Payload != nil {
		t.Error("nil payload must stay nil")
	}
}
