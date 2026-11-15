package llm

import (
	"reflect"
	"strconv"
	"strings"

	"github.com/Pegasus8/piworker/internal/types"
)

// MessageValue returns the value at a message path: "payload",
// "payload.a.b", "payload.items.0" (a list index), "meta.x" (and deeper)
// or "topic". The bool reports whether the path names a value.
func MessageValue(msg *types.Message, path string) (any, bool) {
	if msg == nil || path == "" {
		return nil, false
	}
	parts := strings.Split(path, ".")
	switch parts[0] {
	case "payload":
		return walkPath(msg.Payload, parts[1:])
	case "meta":
		if len(parts) < 2 || msg.Meta == nil {
			return nil, false
		}
		v, ok := msg.Meta[parts[1]]
		if !ok {
			return nil, false
		}
		return walkPath(v, parts[2:])
	case "topic":
		if len(parts) != 1 {
			return nil, false
		}
		return msg.Topic, true
	}
	return nil, false
}

// walkPath follows keys (object members or list indexes) from v.
func walkPath(v any, keys []string) (any, bool) {
	for _, key := range keys {
		if key == "" {
			return nil, false
		}
		switch cur := v.(type) {
		case map[string]any:
			next, ok := cur[key]
			if !ok {
				return nil, false
			}
			v = next
		case []any:
			i, ok := index(key, len(cur))
			if !ok {
				return nil, false
			}
			v = cur[i]
		default:
			next, ok := reflectStep(v, key)
			if !ok {
				return nil, false
			}
			v = next
		}
	}
	return v, true
}

// reflectStep is walkPath's step for other maps with string keys and other
// slices (a node may produce []string, map[string]string, …).
func reflectStep(v any, key string) (any, bool) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return nil, false
		}
		item := rv.MapIndex(reflect.ValueOf(key).Convert(rv.Type().Key()))
		if !item.IsValid() {
			return nil, false
		}
		return item.Interface(), true
	case reflect.Slice, reflect.Array:
		i, ok := index(key, rv.Len())
		if !ok {
			return nil, false
		}
		return rv.Index(i).Interface(), true
	}
	return nil, false
}

func index(key string, n int) (int, bool) {
	i, err := strconv.Atoi(key)
	if err != nil || i < 0 || i >= n {
		return 0, false
	}
	return i, true
}

// SetResult stores a node's result in msg: field "" replaces the payload;
// otherwise payload[field] is set (dots in field are a path), turning a
// payload that is not an object into {"input": <old payload>, field: value}
// and a value along the path that is not an object into one. The maps along
// the path are copied, never changed in place: they may be shared with
// another message.
func SetResult(msg *types.Message, field string, value any) {
	if field == "" {
		msg.Payload = value
		msg.PayloadType = dataTypeOf(value)
		return
	}
	root, ok := msg.Payload.(map[string]any)
	if ok {
		root = copyMap(root)
	} else {
		root = map[string]any{"input": msg.Payload}
	}
	keys := strings.Split(field, ".")
	cur := root
	for _, key := range keys[:len(keys)-1] {
		next, ok := cur[key].(map[string]any)
		if ok {
			next = copyMap(next)
		} else {
			next = map[string]any{}
		}
		cur[key] = next
		cur = next
	}
	cur[keys[len(keys)-1]] = value
	msg.Payload = root
	msg.PayloadType = types.DataTypeObject
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// dataTypeOf is the payload type hint of v.
func dataTypeOf(v any) types.DataType {
	switch v.(type) {
	case string:
		return types.DataTypeString
	case int, int64, float64, float32:
		return types.DataTypeNumber
	case bool:
		return types.DataTypeBoolean
	case []any:
		return types.DataTypeArray
	case map[string]any:
		return types.DataTypeObject
	default:
		return types.DataTypeAny
	}
}
