package service

import (
	"encoding/json"

	"github.com/Pegasus8/piworker/internal/node"
)

// Default-deny redaction of node config for agents (docs/design/mcp.md §6).

// truncatedMark ends a value cut to its bound, or replaces an object or list
// too large to show.
const truncatedMark = "<truncated>"

// Bounds of shown config values.
const (
	// maxVisibleValueBytes bounds one shown config value: a longer string
	// is cut (ending with truncatedMark), a larger object or list (by its
	// JSON text) is replaced by truncatedMark.
	maxVisibleValueBytes = 8 << 10
	// maxConfigKeyBytes bounds a config key name.
	maxConfigKeyBytes = 128
)

// configView returns a node's config as agents see it. A property is shown
// only when the node type's schema declares it AgentVisible and not
// Sensitive; every other present value (including properties the schema
// does not declare, every property of an unknown type, and whole nested
// objects or lists) is replaced by the "<redacted>" sentinel, which drafts
// read as "keep the stored value". Known secret values are redacted from
// what is shown, keys included, and shown values are bounded.
func configView(red secretRedactor, schema *node.ConfigSchema, cfg map[string]any) map[string]any {
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		key := capText(red.text(k), maxConfigKeyBytes)
		p, declared := node.ConfigProperty{}, false
		if schema != nil {
			p, declared = schema.Properties[k]
		}
		if !declared || !p.AgentVisible || p.Sensitive {
			out[key] = redactedMark
			continue
		}
		out[key] = visibleValue(red, v)
	}
	return out
}

// visibleValue is a shown config value: secrets redacted, bounded.
func visibleValue(red secretRedactor, v any) any {
	switch x := redactValue(red, v).(type) {
	case string:
		return cutText(x, maxVisibleValueBytes)
	case map[string]any, []any:
		data, err := marshalNoEscape(x)
		if err != nil || len(data) > maxVisibleValueBytes {
			return truncatedMark
		}
		return x
	default:
		return x
	}
}

// redactValue deep-copies a JSON-like value, redacting known secret values
// from its strings and object keys. A number whose text holds a secret
// becomes the sentinel. Values of other Go types (configs built in memory
// rather than decoded) are normalized through JSON first.
func redactValue(red secretRedactor, v any) any {
	switch x := v.(type) {
	case nil, bool:
		return x
	case string:
		return red.text(x)
	case float64, json.Number:
		data, err := json.Marshal(x)
		if err != nil || red.text(string(data)) != string(data) {
			return redactedMark
		}
		return x
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[red.text(k)] = redactValue(red, e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = redactValue(red, e)
		}
		return out
	}
	data, err := json.Marshal(v)
	if err != nil {
		return redactedMark
	}
	var norm any
	if err := json.Unmarshal(data, &norm); err != nil {
		return redactedMark
	}
	return redactValue(red, norm)
}

// cutText bounds s to limit bytes; a cut text ends with "…<truncated>".
func cutText(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	const tail = "…" + truncatedMark
	return utf8Prefix(s, limit-len(tail)) + tail
}
