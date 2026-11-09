package mcpserver

import (
	"bytes"
	"encoding/json/jsontext"
	"fmt"
)

// Bounds of a message's JSON, checked before anything decodes it
// (docs/design/mcp.md §8). The body limit bounds a message's bytes, not what
// decoding it costs: a decoded value costs from a few dozen bytes (a number)
// to a few hundred (an empty object becomes a Go map), each time the SDK or
// the endpoint decodes it (the SDK decodes params._meta twice on every
// request and a call's arguments once more before the tool sees them), so 1
// MiB of "{}" cost over 100 MiB a request.
const (
	// MaxMessageValues bounds the JSON values and object member names of a
	// message. The largest flows the author tools take (200 nodes, 400
	// connections and their settings) need about a third of it.
	MaxMessageValues = 32 << 10
	// MaxMessageDepth bounds how deep a message's arrays and objects nest.
	MaxMessageDepth = 64
	// MaxMetaBytes bounds params._meta (progress tokens and client hints).
	MaxMetaBytes = 8 << 10
)

// rejectedTooLarge is the mcp_rejections reason of a message refused by
// scanMessage.
const rejectedTooLarge = "request_too_large"

// scanMessage checks a message's JSON against the bounds above without
// decoding it (it reads tokens in place, allocating only for the few member
// names it compares) and returns why it is refused ("" when it is not).
// Syntax errors are left to the strict parse that follows.
func scanMessage(body []byte) string {
	dec := jsontext.NewDecoder(bytes.NewBuffer(body), jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
	var (
		values     int
		topName    string // the member of the top-level object being read
		inParams   bool   // reading the params object (depth 2)
		metaNext   bool   // the next value is params._meta
		inMeta     bool   // reading params._meta (an object or array)
		metaOffset int64  // where params._meta starts
	)
	metaTooLarge := fmt.Sprintf("params._meta exceeds %d bytes", MaxMetaBytes)
	for {
		before := dec.InputOffset()
		tok, err := dec.ReadToken()
		if err != nil {
			return "" // the end, or a syntax error the strict parse reports
		}
		depth := dec.StackDepth()
		kind := tok.Kind()
		if kind == '}' || kind == ']' {
			switch {
			case inMeta && depth == 2:
				inMeta = false
				if dec.InputOffset()-metaOffset > MaxMetaBytes {
					return metaTooLarge
				}
			case inParams && depth == 1:
				inParams = false
			}
			continue
		}
		values++
		if values > MaxMessageValues {
			return fmt.Sprintf("the message holds more than %d JSON values", MaxMessageValues)
		}
		if depth > MaxMessageDepth {
			return fmt.Sprintf("the message's arrays and objects nest deeper than %d levels", MaxMessageDepth)
		}
		if inMeta && dec.InputOffset()-metaOffset > MaxMetaBytes {
			return metaTooLarge
		}
		parentDepth, opens := depth, kind == '{' || kind == '['
		if opens {
			parentDepth--
		}
		parent, length := dec.StackIndex(parentDepth)
		if kind == '"' && parent == '{' && length%2 == 1 {
			// A member name: only those of the top-level object and of
			// params are looked at.
			switch {
			case depth == 1:
				topName = tok.String()
			case inParams && depth == 2:
				metaNext = tok.String() == "_meta"
			}
			continue
		}
		if metaNext {
			metaNext = false
			if opens {
				inMeta, metaOffset = true, dec.InputOffset()-1
			} else if dec.InputOffset()-before > MaxMetaBytes {
				return metaTooLarge
			}
		}
		if parentDepth == 1 && topName == "params" && kind == '{' {
			inParams = true
		}
	}
}
