package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"
)

// redactedMark replaces a known secret value in agent-facing output.
const redactedMark = "<redacted>"

// minSecretLength is the shortest secret value (and encoded form) that is
// redacted: shorter ones would mangle ordinary text and protect nothing.
const minSecretLength = 4

// SecretSource lists the known secret values (*secrets.Store).
type SecretSource interface {
	Names() []string
	Get(name string) (string, bool)
}

// secretRedactor replaces every known secret value, in its literal,
// URL-encoded (query and path) and base64 (standard and URL alphabets, any
// alignment inside a longer encoded text) forms. The zero value redacts
// nothing.
type secretRedactor struct {
	r *strings.Replacer
}

// newSecretRedactor snapshots the current secret values of src.
func newSecretRedactor(src SecretSource) secretRedactor {
	if src == nil {
		return secretRedactor{}
	}
	seen := map[string]bool{}
	var patterns []string
	add := func(p string) {
		if len(p) >= minSecretLength && !seen[p] {
			seen[p] = true
			patterns = append(patterns, p)
		}
	}
	for _, name := range src.Names() {
		v, ok := src.Get(name)
		if !ok || len(v) < minSecretLength {
			continue
		}
		for _, p := range matchedForms(v) {
			add(p)
		}
	}
	if len(patterns) == 0 {
		return secretRedactor{}
	}
	// Longest first, so a longer form wins over a shorter one at the same
	// position (strings.Replacer tries its arguments in order).
	sort.SliceStable(patterns, func(a, b int) bool { return len(patterns[a]) > len(patterns[b]) })
	pairs := make([]string, 0, 2*len(patterns))
	for _, p := range patterns {
		pairs = append(pairs, p, redactedMark)
	}
	return secretRedactor{r: strings.NewReplacer(pairs...)}
}

// matchedForms returns the forms of a secret value v the redactor matches
// (those shorter than minSecretLength are not matched): literal,
// URL-encoded (query and path) and base64 (base64Forms).
func matchedForms(v string) []string {
	return append([]string{v, url.QueryEscape(v), url.PathEscape(v)}, base64Forms(v)...)
}

// unsentSecrets is src without the secrets an agent sent itself: those of
// which a form the redactor matches (matchedForms) appears in one of sent
// (the strings, object keys and numbers of its arguments, sentTexts). An
// agent's tool results are redacted with it, so what it sent comes back as
// it sent it, and whether a result shows "<redacted>" never confirms a
// guess (ADR-0021): a right guess and a wrong one are echoed alike, and a
// secret the flow outputs of its own is still redacted unless the agent
// sent its value.
func unsentSecrets(src SecretSource, sent []string) SecretSource {
	if src == nil {
		return nil
	}
	var out valueList
	for _, name := range src.Names() {
		v, ok := src.Get(name)
		if !ok || len(v) < minSecretLength {
			continue
		}
		if !sentAny(matchedForms(v), sent) {
			out = append(out, v)
		}
	}
	return out
}

// sentAny reports whether a form (of at least minSecretLength bytes)
// appears in one of sent.
func sentAny(forms, sent []string) bool {
	for _, f := range forms {
		if len(f) < minSecretLength {
			continue
		}
		for _, s := range sent {
			if strings.Contains(s, f) {
				return true
			}
		}
	}
	return false
}

// sentTexts lists the texts of an agent's arguments as a result can repeat
// them: every string, object key and number (in its JSON form, which is
// what a reply shows), at any depth.
func sentTexts(args map[string]any) []string {
	if len(args) == 0 {
		return nil
	}
	data, err := marshalNoEscape(args)
	if err != nil {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil
	}
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			out = append(out, x)
		case json.Number:
			out = append(out, x.String())
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			for k, e := range x {
				out = append(out, k)
				walk(e)
			}
		}
	}
	walk(v)
	return out
}

// base64Forms returns, for each alphabet and each of the three byte
// alignments v can have inside a longer encoded text, the run of base64
// characters that depend on v's bytes only. It matches v encoded on its own
// (padded or not) and v embedded in a larger encoded value (e.g. "user:v" in
// a Basic authorization header).
func base64Forms(v string) []string {
	var out []string
	for _, enc := range []*base64.Encoding{base64.RawStdEncoding, base64.RawURLEncoding} {
		for k := range 3 {
			buf := append(make([]byte, k), v...)
			e := enc.EncodeToString(buf)
			start := (8*k + 5) / 6        // first character fully inside v
			end := (8 * (k + len(v))) / 6 // past the last one
			if end > len(e) {
				end = len(e)
			}
			if end-start >= minSecretLength {
				out = append(out, e[start:end])
			}
		}
	}
	return out
}

// text redacts a string.
func (s secretRedactor) text(v string) string {
	if s.r == nil {
		return v
	}
	return s.r.Replace(v)
}

// json redacts every string (and object key) of a JSON document, after
// decoding it, so JSON escaping cannot hide a secret. A document without
// secrets is returned unchanged.
func (s secretRedactor) json(raw json.RawMessage) json.RawMessage {
	if s.r == nil || len(raw) == 0 {
		return raw
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		// Not JSON (never produced by the runtime): redact it as text and
		// keep it only if it is still valid.
		if out := s.text(string(raw)); json.Valid([]byte(out)) {
			return json.RawMessage(out)
		}
		return json.RawMessage(`"` + redactedMark + `"`)
	}
	nv, changed := s.walk(v)
	if !changed {
		return raw
	}
	out, err := marshalNoEscape(nv)
	if err != nil {
		return json.RawMessage(`"` + redactedMark + `"`)
	}
	return out
}

func (s secretRedactor) walk(v any) (any, bool) {
	switch x := v.(type) {
	case string:
		out := s.text(x)
		return out, out != x
	case json.Number:
		if s.text(x.String()) != x.String() {
			return redactedMark, true
		}
		return x, false
	case []any:
		changed := false
		for i, e := range x {
			ne, c := s.walk(e)
			x[i] = ne
			changed = changed || c
		}
		return x, changed
	case map[string]any:
		changed := false
		out := make(map[string]any, len(x))
		for k, e := range x {
			nk := s.text(k)
			ne, c := s.walk(e)
			out[nk] = ne
			changed = changed || c || nk != k
		}
		return out, changed
	}
	return v, false
}

// marshalNoEscape encodes v without HTML escaping (so "<redacted>" stays
// readable) and without the encoder's trailing newline.
func marshalNoEscape(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// truncatedResult replaces a result larger than max_result_bytes.
type truncatedResult struct {
	Truncated  bool   `json:"truncated"`
	Preview    string `json:"preview"`
	TotalBytes int    `json:"totalBytes"`
}

// capResult keeps a JSON result within limit bytes: a larger one becomes
// {"truncated": true, "preview": <the start of its JSON text>, "totalBytes":
// <its size>}, which is valid JSON of at most limit bytes.
func capResult(raw json.RawMessage, limit int) json.RawMessage {
	if len(raw) == 0 || limit <= 0 || len(raw) <= limit {
		return raw
	}
	text := string(raw)
	for n := limit; n > 0; {
		out, err := marshalNoEscape(truncatedResult{Truncated: true, Preview: utf8Prefix(text, n), TotalBytes: len(raw)})
		if err != nil {
			break
		}
		if len(out) <= limit {
			return out
		}
		n -= max(len(out)-limit, 1)
	}
	return json.RawMessage(`{"truncated":true}`)
}

// utf8Prefix returns at most n bytes of s, cut at a rune boundary.
func utf8Prefix(s string, n int) string {
	if n >= len(s) {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// capText bounds an error message (bytes, cut at a rune boundary).
func capText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return utf8Prefix(s, n-len("…")) + "…"
}
