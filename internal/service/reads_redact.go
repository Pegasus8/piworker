package service

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

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
	return valuesRedactor(append(r.secretValues(), extra...))
}

// secretValues snapshots the secret store's values.
func (r *Reads) secretValues() []string {
	var values []string
	for _, name := range r.secrets.Names() {
		if v, ok := r.secrets.Get(name); ok {
			values = append(values, v)
		}
	}
	return values
}

// maxFormGrowth bounds how many times longer than a value its longest form
// a valuesRedactor matches can be (an escaped form, 6 bytes per byte at
// most, URL-encoded, 3 per byte).
const maxFormGrowth = 24

// longestForm bounds the length of the longest text valuesRedactor(values)
// matches.
func longestForm(values ...[]string) int {
	longest := 0
	for _, list := range values {
		for _, v := range list {
			longest = max(longest, len(v))
		}
	}
	return maxFormGrowth * longest
}

// valuesRedactor redacts values in their literal, URL-encoded and base64
// forms (secretRedactor) and as they read inside JSON or Go-quoted text.
func valuesRedactor(values []string) secretRedactor {
	src := make(valueList, 0, 5*len(values))
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

// minHiddenPartBytes is the shortest part of a hidden setting (the whole
// value, a word, a URL's path segment or query value) that hiddenValues
// returns: shorter parts are no credentials, and redacting them would
// mangle common words. A URL's password is returned from minSecretLength.
const minHiddenPartBytes = 8

// hiddenValues returns what GetFlow hides of f's settings in the forms
// recorded text (node errors) may repeat it other than as a URL, which
// hideURLsInText already cuts after its host: the string values, at any
// depth, of every property agents may not see (Sensitive or not
// AgentVisible, undeclared, or of an unknown node type). Each value is
// split around its {{…}} templates (references, not values); each literal
// part is returned whole, with its words outside URLs and, for the URLs in
// it, their path, path segments, query values, fragment and userinfo (not
// their scheme and host, which errors show for diagnosis).
func (r *Reads) hiddenValues(f *flow.Flow) []string {
	var out []string
	add := func(s string, minLen int) {
		if len(s) >= minLen {
			out = append(out, s)
		}
	}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			hiddenParts(x, add)
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
		var schema *node.ConfigSchema
		if info, ok := r.registry.Get(n.Type); ok {
			schema = info.Config
		}
		for name, v := range n.Config {
			var p node.ConfigProperty
			declared := false
			if schema != nil {
				p, declared = schema.Properties[name]
			}
			if declared && p.AgentVisible && !p.Sensitive {
				continue // shown by GetFlow
			}
			walk(redactValue(secretRedactor{}, v))
		}
	}
	return out
}

// hiddenParts passes add the parts of a hidden setting's value s that
// hiddenValues returns, with the shortest length each may have.
func hiddenParts(s string, add func(part string, minLen int)) {
	for _, lit := range literalParts(s) {
		add(lit, minHiddenPartBytes)
		// A URL's words are its parts below: its scheme and host stay
		// shown (hideURLsInText keeps them for diagnosis).
		for _, w := range strings.FieldsFunc(urlInViewText.ReplaceAllString(lit, " "), isWordBreak) {
			add(w, minHiddenPartBytes)
		}
		for _, m := range urlInViewText.FindAllString(lit, -1) {
			u, err := url.Parse(strings.TrimRight(m, ".,;:!?)]}"))
			if err != nil {
				continue
			}
			add(u.Path, minHiddenPartBytes)
			add(u.EscapedPath(), minHiddenPartBytes)
			for _, seg := range strings.Split(u.EscapedPath(), "/") {
				add(seg, minHiddenPartBytes)
				if unescaped, err := url.PathUnescape(seg); err == nil {
					add(unescaped, minHiddenPartBytes)
				}
			}
			for _, values := range u.Query() {
				for _, v := range values {
					add(v, minHiddenPartBytes)
				}
			}
			add(u.Fragment, minHiddenPartBytes)
			if u.User != nil {
				add(u.User.Username(), minHiddenPartBytes)
				if pw, ok := u.User.Password(); ok {
					add(pw, minSecretLength)
				}
			}
		}
	}
}

// literalParts splits s around its {{…}} templates; an unclosed template's
// text counts as literal.
func literalParts(s string) []string {
	var out []string
	for {
		open := strings.Index(s, "{{")
		if open < 0 {
			return append(out, s)
		}
		out = append(out, s[:open])
		end := strings.Index(s[open+2:], "}}")
		if end < 0 {
			return append(out, s[open+2:])
		}
		s = s[open+2+end+2:]
	}
}

// isWordBreak separates the words of a hidden setting: spaces, quotes,
// path separators and the punctuation around values in commands, headers,
// queries and JSON.
func isWordBreak(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune("\"'`=,;&?#()[]{}<>|/\\", r)
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

// urlInViewText matches a URL with a scheme, up to whitespace, a quote, an
// angle bracket or a backquote; a "<redacted>" an earlier redaction left in
// it (a node's own query redaction) is part of it.
var urlInViewText = regexp.MustCompile("(?i)\\b[a-z][a-z0-9+.-]*://(?:[^\\s\"'<>`]|" + regexp.QuoteMeta(redactedMark) + ")+")

// hideURLsInText rewrites every URL in s that has anything past its host
// (userinfo, a path, a query or a fragment) to scheme://host/<redacted>:
// the read views hide the URLs of node settings (they are not
// AgentVisible), and a credential may sit in any of those parts, the path
// included (Telegram bot, Discord and Slack webhook URLs). Trailing
// sentence punctuation is kept out of the URL; a URL that cannot be parsed
// keeps only its scheme.
func hideURLsInText(s string) string {
	if !strings.Contains(s, "://") {
		return s
	}
	return urlInViewText.ReplaceAllStringFunc(s, func(m string) string {
		trimmed := strings.TrimRight(m, ".,;:!?)]}")
		tail := m[len(trimmed):]
		u, err := url.Parse(trimmed)
		if err != nil {
			return strings.ToLower(trimmed[:strings.Index(trimmed, "://")]) + "://" + redactedMark + tail
		}
		if u.User == nil && (u.Path == "" || u.Path == "/") && u.RawPath == "" && u.RawQuery == "" && !u.ForceQuery &&
			u.Fragment == "" && u.RawFragment == "" {
			return m
		}
		return u.Scheme + "://" + u.Host + "/" + redactedMark + tail
	})
}
