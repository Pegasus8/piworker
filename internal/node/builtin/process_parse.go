package builtin

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
)

// ParseProcess parses a string payload from a structured text format (JSON or
// CSV) into a native value, so downstream nodes can work with fields instead of
// raw text.
type ParseProcess struct {
	*node.BaseNode

	format    string
	delimiter string
	header    bool
}

// NewParseProcess creates a ParseProcess from configuration.
func NewParseProcess(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)
	format := strings.ToLower(base.GetConfigString("format", "json"))
	if format != "json" && format != "csv" {
		return nil, fmt.Errorf("unsupported format %q (use json or csv)", format)
	}
	delimiter := base.GetConfigString("delimiter", ",")
	if delimiter == "" {
		delimiter = ","
	}
	return &ParseProcess{
		BaseNode:  base,
		format:    format,
		delimiter: delimiter,
		header:    base.GetConfigBool("header", true),
	}, nil
}

// Process parses the (string) payload according to the configured format.
func (p *ParseProcess) Process(_ context.Context, msg *types.Message) ([]*types.Message, error) {
	raw, ok := asString(msg.Payload)
	if !ok {
		return nil, fmt.Errorf("parse expects a string payload, got %T", msg.Payload)
	}

	var parsed interface{}
	var err error
	switch p.format {
	case "json":
		parsed, err = parseJSON(raw)
	case "csv":
		parsed, err = p.parseCSV(raw)
	}
	if err != nil {
		return nil, fmt.Errorf("%s parse failed: %w", p.format, err)
	}

	out := msg.Clone()
	out.Payload = parsed
	out.PayloadType = inferDataType(parsed)
	out.SourcePort = "output"
	out.SetMeta("parsed", p.format)
	return []*types.Message{out}, nil
}

func asString(v interface{}) (string, bool) {
	switch s := v.(type) {
	case string:
		return s, true
	case []byte:
		return string(s), true
	default:
		return "", false
	}
}

func parseJSON(raw string) (interface{}, error) {
	var v interface{}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, err
	}
	return v, nil
}

// parseCSV returns []map[string]interface{} (keyed by the header row) when
// header is true, otherwise [][]string.
func (p *ParseProcess) parseCSV(raw string) (interface{}, error) {
	reader := csv.NewReader(strings.NewReader(raw))
	reader.Comma = []rune(p.delimiter)[0]
	reader.FieldsPerRecord = -1 // tolerate ragged rows
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if !p.header {
		return records, nil
	}
	if len(records) == 0 {
		return []map[string]interface{}{}, nil
	}
	headers := records[0]
	rows := make([]map[string]interface{}, 0, len(records)-1)
	for _, rec := range records[1:] {
		row := make(map[string]interface{}, len(headers))
		for i, h := range headers {
			if i < len(rec) {
				row[h] = rec[i]
			} else {
				row[h] = ""
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// Ports returns the port definitions.
func (p *ParseProcess) Ports() (inputs []types.Port, outputs []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeString, Required: true}},
		[]types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}}
}

// Validate checks the configuration.
func (p *ParseProcess) Validate() error {
	if p.format != "json" && p.format != "csv" {
		return fmt.Errorf("unsupported format %q", p.format)
	}
	return nil
}

func parseProcessConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"format": {
				Type:        "string",
				Title:       "Format",
				Description: "Input text format to parse",
				Default:     "json",
				Enum:        []string{"json", "csv"},
			},
			"header": {
				Type:        "boolean",
				Title:       "CSV has header row",
				Description: "When parsing CSV, treat the first row as column names (produces objects)",
				Default:     true,
			},
			"delimiter": {
				Type:        "string",
				Title:       "CSV delimiter",
				Description: "Field delimiter for CSV (default ,)",
				Default:     ",",
			},
		},
		Required: []string{"format"},
	}
}

// GetConfigSchema returns the configuration schema for the UI.
func (p *ParseProcess) GetConfigSchema() node.ConfigSchema {
	return parseProcessConfigSchema()
}

// ParseProcessInfo returns the node type info for registration.
func ParseProcessInfo() node.NodeTypeInfo {
	schema := parseProcessConfigSchema()
	return node.NodeTypeInfo{
		Type:        "process-parse",
		Name:        "Parse",
		Description: "Parse a JSON or CSV string payload into structured data",
		Documentation: `## Parse

Parses a string payload from JSON or CSV into a native value so downstream nodes
can address fields directly.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| format | enum | json | ` + "`json`" + ` or ` + "`csv`" + ` |
| header | boolean | true | CSV only: first row is column names (rows become objects) |
| delimiter | string | , | CSV field delimiter |

## Output

- ` + "`json`" + `: the parsed object/array/scalar.
- ` + "`csv`" + ` with header: an array of objects keyed by the header row.
- ` + "`csv`" + ` without header: an array of string arrays.

## Use Cases

- Turn a webhook's raw JSON body into addressable fields
- Read a CSV file's contents into rows
`,
		Category: types.NodeCategoryProcessing,
		Inputs:   []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeString, Required: true}},
		Outputs:  []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}},
		Config:   &schema,
		Icon:     "braces",
	}
}

func init() {
	info := ParseProcessInfo()
	if err := node.Register(info, NewParseProcess); err != nil {
		panic("failed to register parse process: " + err.Error())
	}
}
