package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"strconv"
	"strings"
)

type messageKind int

const (
	kindRequest messageKind = iota + 1
	kindNotification
	kindResponse // the client's answer to a request from the server
)

// message is a JSON-RPC message read from stdin.
type message struct {
	raw    []byte // forwarded as is
	kind   messageKind
	id     json.RawMessage
	key    string // the normalized id
	method string
	params json.RawMessage
}

// parseMessage classifies one line from stdin. When it is not a message the
// relay can forward, it returns the JSON-RPC error response to write instead.
func parseMessage(line []byte) (*message, []byte) {
	if !json.Valid(line) {
		return nil, errorResponse(nil, codeParseError, "parse error: the message is not valid JSON", nil)
	}
	if line[0] == '[' {
		return nil, errorResponse(nil, codeInvalidRequest, "invalid request: JSON-RPC batches are not supported", nil)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(line, &fields) != nil || fields == nil {
		return nil, errorResponse(nil, codeInvalidRequest, "invalid request: a JSON-RPC message must be an object", nil)
	}
	m := &message{raw: line, params: fields["params"]}
	rawID, hasID := fields["id"]
	var echoID json.RawMessage // only a valid id is echoed in errors
	if key, ok := idKey(rawID); hasID && ok {
		m.id, m.key, echoID = rawID, key, rawID
	}
	var version string
	if json.Unmarshal(fields["jsonrpc"], &version) != nil || version != "2.0" {
		return nil, errorResponse(echoID, codeInvalidRequest, `invalid request: "jsonrpc" must be "2.0"`, nil)
	}
	rawMethod, hasMethod := fields["method"]
	if hasMethod && (json.Unmarshal(rawMethod, &m.method) != nil || m.method == "") {
		return nil, errorResponse(echoID, codeInvalidRequest, `invalid request: "method" must be a non-empty string`, nil)
	}
	_, hasResult := fields["result"]
	_, hasError := fields["error"]
	switch {
	case hasMethod && !hasID:
		m.kind = kindNotification
	case hasID && m.key == "":
		return nil, errorResponse(nil, codeInvalidRequest, "invalid request: the id must be a string or a number", nil)
	case hasMethod:
		m.kind = kindRequest
	case hasID && (hasResult || hasError):
		m.kind = kindResponse
	default:
		return nil, errorResponse(echoID, codeInvalidRequest, "invalid request: the message is neither a request, a notification nor a response", nil)
	}
	return m, nil
}

// label names the message in logs (never its parameters).
func (m *message) label() string {
	if m.method != "" {
		return m.method
	}
	return "response " + string(m.id)
}

// metaVersion is the per-request protocol version (2026-07-28 and later).
func (m *message) metaVersion() string {
	var p struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if json.Unmarshal(m.params, &p) != nil {
		return ""
	}
	var v string
	if json.Unmarshal(p.Meta[metaProtocolVersion], &v) != nil || !validVersion(v) {
		return ""
	}
	return v
}

// name is what the Mcp-Name header carries: the tool or prompt name, or the
// resource URI.
func (m *message) name() string {
	var p struct {
		Name string `json:"name"`
		URI  string `json:"uri"`
	}
	if json.Unmarshal(m.params, &p) != nil {
		return ""
	}
	switch m.method {
	case "tools/call", "prompts/get":
		return p.Name
	case "resources/read":
		return p.URI
	}
	return ""
}

// validVersion accepts protocol version strings safe to send in a header.
func validVersion(v string) bool {
	if v == "" || len(v) > 32 {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && c != '-' && c != '.' && c != '_' {
			return false
		}
	}
	return true
}

// isHeaderValue reports whether s can be sent as an HTTP header value as is:
// no control characters other than tab, no leading or trailing whitespace.
func isHeaderValue(s string) bool {
	if s != strings.TrimSpace(s) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < 0x20 && c != '\t') || c == 0x7f {
			return false
		}
	}
	return true
}

// idKey normalizes a JSON-RPC id (a string or a number) so that the same id
// written differently (7, 7.0) matches. Other values are not valid ids.
func idKey(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return "", false
	}
	switch v := v.(type) {
	case string:
		return "s:" + v, true
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return "n:" + strconv.FormatInt(i, 10), true
		}
		f, err := v.Float64()
		if err != nil || math.IsInf(f, 0) {
			return "", false
		}
		if f == math.Trunc(f) && math.Abs(f) < 1<<63 {
			return "n:" + strconv.FormatInt(int64(f), 10), true
		}
		return "n:" + strconv.FormatFloat(f, 'g', -1, 64), true
	}
	return "", false
}

// answer is a JSON-RPC message from PiWorker.
type answer struct {
	id         json.RawMessage
	key        string
	result     json.RawMessage
	fromServer bool // a request or notification, not an answer
}

func parseAnswer(data []byte) (*answer, error) {
	notMessage := &bridgeError{"PiWorker's answer is not a JSON-RPC message"}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return nil, notMessage
	}
	var env struct {
		ID     json.RawMessage `json:"id"`
		Method json.RawMessage `json:"method"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if json.Unmarshal(data, &env) != nil {
		return nil, notMessage
	}
	if env.Method != nil {
		return &answer{fromServer: true}, nil
	}
	key, ok := idKey(env.ID)
	if !ok || (env.Result == nil && env.Error == nil) {
		return nil, notMessage
	}
	return &answer{id: env.ID, key: key, result: env.Result}, nil
}

// rpcError is a JSON-RPC error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// errorResponse builds a JSON-RPC error response; a nil id is written as null.
func errorResponse(id json.RawMessage, code int, msg string, data any) []byte {
	if id == nil {
		id = json.RawMessage("null")
	}
	b, err := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   rpcError        `json:"error"`
	}{"2.0", id, rpcError{Code: code, Message: msg, Data: data}})
	if err != nil {
		// Only possible with unmarshalable data; answer without it.
		b, _ = json.Marshal(struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Error   rpcError        `json:"error"`
		}{"2.0", id, rpcError{Code: code, Message: msg}})
	}
	return b
}

var errLineTooLong = errors.New("line too long")

// readLine reads one line without its line ending. A line longer than max is
// consumed and reported as errLineTooLong; the reading can go on after it.
func readLine(br *bufio.Reader, max int) ([]byte, error) {
	var line []byte
	tooLong := false
	for {
		chunk, err := br.ReadSlice('\n')
		if !tooLong {
			if len(line)+len(chunk) > max+2 { // room for "\r\n"
				tooLong, line = true, nil
			} else {
				line = append(line, chunk...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if tooLong {
			return nil, errLineTooLong
		}
		line = bytes.TrimSuffix(bytes.TrimSuffix(line, []byte("\n")), []byte("\r"))
		if len(line) > max {
			return nil, errLineTooLong
		}
		if err != nil && (len(line) == 0 || !errors.Is(err, io.EOF)) {
			return nil, err
		}
		// A last line without a newline is returned now and EOF next time.
		return line, nil
	}
}

// scanEvents reads a text/event-stream body and calls onData with the data of
// each message event until onData asks to stop.
func scanEvents(body io.Reader, max int, onData func([]byte) (stop bool, err error)) error {
	br := bufio.NewReaderSize(body, 32<<10)
	var (
		data    []byte
		hasData bool
		event   string
	)
	dispatch := func() (bool, error) {
		defer func() { data, hasData, event = nil, false, "" }()
		if !hasData || (event != "" && event != "message") {
			return false, nil
		}
		return onData(data)
	}
	tooLarge := &bridgeError{fmt.Sprintf("PiWorker's answer is larger than %d bytes", max)}
	for {
		line, err := readLine(br, max)
		if errors.Is(err, errLineTooLong) {
			return tooLarge
		}
		if errors.Is(err, io.EOF) {
			_, err := dispatch()
			return err
		}
		if err != nil {
			return fmt.Errorf("reading PiWorker's event stream: %w", err)
		}
		if len(line) == 0 {
			if stop, err := dispatch(); stop || err != nil {
				return err
			}
			continue
		}
		if line[0] == ':' {
			continue
		}
		field, value, _ := bytes.Cut(line, []byte(":"))
		value = bytes.TrimPrefix(value, []byte(" "))
		switch string(field) {
		case "data":
			if hasData {
				data = append(data, '\n')
			}
			if len(data)+len(value) > max {
				return tooLarge
			}
			data, hasData = append(data, value...), true
		case "event":
			event = string(value)
		}
	}
}

// mediaType is the lower-case media type of a Content-Type header.
func mediaType(contentType string) string {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	return mt
}
