package service

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
)

// Default-deny redaction of node config for agents (docs/design/mcp.md §6).

// redactor snapshots the secret values the read views redact: the secret
// store's values and extra (e.g. a flow's Sensitive config values), each
// in its literal, URL-encoded and base64 forms (secretRedactor) and also as
// it reads inside JSON or Go-quoted text, where recorded errors that echo a
// payload carry it.
func (r *Reads) redactor(extra ...string) secretRedactor {
	var values []string
	for _, name := range r.secrets.Names() {
		if v, ok := r.secrets.Get(name); ok {
			values = append(values, v)
		}
	}
	values = append(values, extra...)
	src := make(valueList, 0, 4*len(values))
	for _, v := range values {
		src = append(src, v)
		src = append(src, escapedForms(v)...)
	}
	return newSecretRedactor(src)
}

// minSensitiveWordBytes is the shortest word of a literal Sensitive value
// that is redacted on its own (the token of "Bearer <token>").
const minSensitiveWordBytes = 8

// sensitiveValues returns the credentials typed literally into the
// Sensitive properties of f's nodes (strings without a {{…}} template, at
// any depth, and their words of at least minSensitiveWordBytes), so the
// views of the flow redact them like stored secrets. Templated values hold
// {{secret.NAME}} references, which are names, not credentials.
func (r *Reads) sensitiveValues(f *flow.Flow) []string {
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			if x == "" || strings.Contains(x, "{{") {
				return
			}
			out = append(out, x)
			if words := strings.Fields(x); len(words) > 1 {
				for _, w := range words {
					if len(w) >= minSensitiveWordBytes {
						out = append(out, w)
					}
				}
			}
		case map[string]any:
			for _, e := range x {
				walk(e)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	for i := range f.Nodes {
		n := &f.Nodes[i]
		info, ok := r.registry.Get(n.Type)
		if !ok || info.Config == nil {
			continue
		}
		for name, p := range info.Config.Properties {
			if v, present := n.Config[name]; present && p.Sensitive {
				walk(redactValue(secretRedactor{}, v))
			}
		}
	}
	return out
}

// escapedForms returns v as it reads inside JSON (with and without HTML
// escaping) and Go-quoted (%q, also ASCII-only) text, without the quotes,
// where that differs from v.
func escapedForms(v string) []string {
	var out []string
	add := func(s string) {
		if s != v {
			out = append(out, s)
		}
	}
	if data, err := json.Marshal(v); err == nil {
		add(strings.TrimSuffix(strings.TrimPrefix(string(data), `"`), `"`))
	}
	if data, err := marshalNoEscape(v); err == nil {
		add(strings.TrimSuffix(strings.TrimPrefix(string(data), `"`), `"`))
	}
	add(strings.TrimSuffix(strings.TrimPrefix(strconv.Quote(v), `"`), `"`))
	add(strings.TrimSuffix(strings.TrimPrefix(strconv.QuoteToASCII(v), `"`), `"`))
	return out
}

// valueList is a SecretSource of bare values.
type valueList []string

func (l valueList) Names() []string {
	names := make([]string, len(l))
	for i := range l {
		names[i] = strconv.Itoa(i)
	}
	return names
}

func (l valueList) Get(name string) (string, bool) {
	i, err := strconv.Atoi(name)
	if err != nil || i < 0 || i >= len(l) {
		return "", false
	}
	return l[i], true
}

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
