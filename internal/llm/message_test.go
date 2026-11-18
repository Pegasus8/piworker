package llm

import (
	"reflect"
	"testing"

	"github.com/Pegasus8/piworker/internal/types"
)

func TestMessageValue(t *testing.T) {
	msg := &types.Message{
		Payload: map[string]any{
			"a":     map[string]any{"b": "deep"},
			"items": []any{"zero", map[string]any{"x": 1.0}},
			"strs":  []string{"s0"},
		},
		Topic: "t",
		Meta:  map[string]any{"x": "meta-x", "nested": map[string]any{"y": true}},
	}
	cases := map[string]any{
		"payload.a.b":       "deep",
		"payload.items.0":   "zero",
		"payload.items.1":   map[string]any{"x": 1.0},
		"payload.items.1.x": 1.0,
		"payload.strs.0":    "s0",
		"meta.x":            "meta-x",
		"meta.nested.y":     true,
		"topic":             "t",
	}
	for path, want := range cases {
		got, ok := MessageValue(msg, path)
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %#v %v, want %#v", path, got, ok, want)
		}
	}
	if got, ok := MessageValue(msg, "payload"); !ok || !reflect.DeepEqual(got, msg.Payload) {
		t.Errorf("payload: %#v %v", got, ok)
	}
	for _, path := range []string{"", "payload.missing", "payload.items.5", "payload.items.x", "payload.a.b.c", "meta.none", "topic.x", "other", "payload."} {
		if got, ok := MessageValue(msg, path); ok {
			t.Errorf("%q: unexpected %#v", path, got)
		}
	}
	if _, ok := MessageValue(nil, "payload"); ok {
		t.Error("nil message")
	}
}

func TestSetResult(t *testing.T) {
	msg := &types.Message{Payload: "hello"}
	SetResult(msg, "", map[string]any{"k": "v"})
	if !reflect.DeepEqual(msg.Payload, map[string]any{"k": "v"}) || msg.PayloadType != types.DataTypeObject {
		t.Fatalf("replace: %#v %s", msg.Payload, msg.PayloadType)
	}
	SetResult(msg, "", "text")
	if msg.Payload != "text" || msg.PayloadType != types.DataTypeString {
		t.Fatalf("replace with text: %#v %s", msg.Payload, msg.PayloadType)
	}

	// A non-object payload becomes {"input": old, field: value}.
	SetResult(msg, "answer", "yes")
	if !reflect.DeepEqual(msg.Payload, map[string]any{"input": "text", "answer": "yes"}) || msg.PayloadType != types.DataTypeObject {
		t.Fatalf("wrap: %#v", msg.Payload)
	}

	// Dots are a path; the original maps are not changed (copy on write).
	inner := map[string]any{"keep": 1}
	orig := map[string]any{"llm": inner, "other": "x"}
	msg = &types.Message{Payload: orig}
	SetResult(msg, "llm.answer", "hi")
	want := map[string]any{"llm": map[string]any{"keep": 1, "answer": "hi"}, "other": "x"}
	if !reflect.DeepEqual(msg.Payload, want) {
		t.Fatalf("path: %#v", msg.Payload)
	}
	if _, changed := inner["answer"]; changed {
		t.Fatal("SetResult changed a map it does not own")
	}
	if _, changed := orig["llm"].(map[string]any)["answer"]; changed {
		t.Fatal("SetResult changed the original payload")
	}
	// A non-object along the path is replaced by an object.
	SetResult(msg, "other.deep", 2)
	if !reflect.DeepEqual(msg.Payload.(map[string]any)["other"], map[string]any{"deep": 2}) {
		t.Fatalf("replace along the path: %#v", msg.Payload)
	}
}
