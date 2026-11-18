package toolschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// maxJSONNesting bounds how deeply arrays and objects may nest in a schema
// document (defaults and examples included), well above what the subset
// needs, so a crafted document cannot make the decoder recurse deeply.
const maxJSONNesting = 64

// decoder reads exactly one JSON value token by token, keeping numbers as
// json.Number and refusing duplicate object keys: encoding/json would keep
// the last one silently, and a client reading the same document might keep
// the first.
type decoder struct {
	dec *json.Decoder
}

// decodeDocument parses raw as a single JSON value.
func decodeDocument(raw []byte) (any, *Issue) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	d := decoder{dec: dec}
	v, issue := d.value("", 0)
	if issue != nil {
		return nil, issue
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, &Issue{Message: "invalid JSON: unexpected data after the schema"}
	}
	return v, nil
}

func (d *decoder) value(ptr string, depth int) (any, *Issue) {
	tok, err := d.dec.Token()
	if err != nil {
		return nil, jsonError(err)
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil // string, json.Number, bool or nil
	}
	if depth >= maxJSONNesting {
		return nil, &Issue{Pointer: ptr, Message: fmt.Sprintf("invalid JSON: nested more than %d levels deep", maxJSONNesting)}
	}
	switch delim {
	case '{':
		m := map[string]any{}
		for d.dec.More() {
			ktok, err := d.dec.Token()
			if err != nil {
				return nil, jsonError(err)
			}
			key, _ := ktok.(string)
			kp := ptr + "/" + escapePointer(key)
			if _, dup := m[key]; dup {
				return nil, &Issue{Pointer: kp, Message: fmt.Sprintf("duplicate key %s", quoteShort(key))}
			}
			v, issue := d.value(kp, depth+1)
			if issue != nil {
				return nil, issue
			}
			m[key] = v
		}
		if _, err := d.dec.Token(); err != nil { // '}'
			return nil, jsonError(err)
		}
		return m, nil
	case '[':
		a := []any{}
		for i := 0; d.dec.More(); i++ {
			v, issue := d.value(ptr+"/"+strconv.Itoa(i), depth+1)
			if issue != nil {
				return nil, issue
			}
			a = append(a, v)
		}
		if _, err := d.dec.Token(); err != nil { // ']'
			return nil, jsonError(err)
		}
		return a, nil
	}
	return nil, &Issue{Pointer: ptr, Message: fmt.Sprintf("invalid JSON: unexpected %v", delim)}
}

func jsonError(err error) *Issue {
	if errors.Is(err, io.EOF) {
		return &Issue{Message: "invalid JSON: unexpected end of the document"}
	}
	return &Issue{Message: "invalid JSON: " + err.Error()}
}

// escapePointer escapes one JSON pointer segment (RFC 6901).
func escapePointer(s string) string {
	if !strings.ContainsAny(s, "~/") {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

// unescapePointer reverses escapePointer.
func unescapePointer(s string) string {
	if !strings.Contains(s, "~") {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "~1", "/"), "~0", "~")
}
