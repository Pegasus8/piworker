package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
)

// TemplateProcess renders a text template from the message, producing a string
// payload. Useful for building notification/log messages.
type TemplateProcess struct {
	*node.BaseNode

	template string
}

// NewTemplateProcess creates a new TemplateProcess from configuration.
func NewTemplateProcess(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)
	tmpl := base.GetConfigString("template", "")
	if tmpl == "" {
		return nil, fmt.Errorf("template is required")
	}
	return &TemplateProcess{BaseNode: base, template: tmpl}, nil
}

// Process renders the template and sets the result as the message payload.
func (p *TemplateProcess) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	rendered := renderTemplate(p.template, msg)

	out := msg.Clone()
	out.Payload = rendered
	out.PayloadType = types.DataTypeString
	out.SourcePort = "output"
	return []*types.Message{out}, nil
}

// Ports returns the port definitions.
func (p *TemplateProcess) Ports() (inputs []types.Port, outputs []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		[]types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeString, Multiple: true}}
}

// Validate checks the configuration.
func (p *TemplateProcess) Validate() error {
	if p.template == "" {
		return fmt.Errorf("template is required")
	}
	return nil
}

func templateProcessConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"template": {
				Type:        "string",
				Title:       "Template",
				Description: "Text with {{payload.field}}, {{topic}}, {{meta.key}}, {{id}}, {{timestamp}} placeholders",
			},
		},
		Required: []string{"template"},
	}
}

// GetConfigSchema returns the configuration schema for UI.
func (p *TemplateProcess) GetConfigSchema() node.ConfigSchema {
	return templateProcessConfigSchema()
}

// TemplateProcessInfo returns the node type info for registration.
func TemplateProcessInfo() node.NodeTypeInfo {
	schema := templateProcessConfigSchema()
	return node.NodeTypeInfo{
		Type:        "process-template",
		Name:        "Template",
		Description: "Render a text template from the message into a string",
		Documentation: `## Template

Renders a text template using values from the message and outputs the rendered
string as the new payload. Great for composing human-readable notification or
log text before an action node.

## Placeholders

| Placeholder | Value |
|-------------|-------|
| {{payload}} | Entire payload as JSON |
| {{payload.field}} | Nested payload field (dot notation) |
| {{meta.key}} | Metadata value |
| {{topic}} | Message topic |
| {{id}} | Message ID |
| {{timestamp}} | ISO timestamp |

## Example

` + "```" + `
Temperature is {{payload.temperature}}°C in {{payload.room}}
` + "```" + `
`,
		Category: types.NodeCategoryProcessing,
		Inputs:   []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		Outputs:  []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeString, Multiple: true}},
		Config:   &schema,
		Icon:     "type",
	}
}

// renderTemplate expands {{...}} placeholders in tmpl using the message. It is
// shared by template-style nodes and reuses the package-level templateRe.
func renderTemplate(tmpl string, msg *types.Message) string {
	return templateRe.ReplaceAllStringFunc(tmpl, func(match string) string {
		varPath := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(match, "{{"), "}}"))
		parts := strings.SplitN(varPath, ".", 2)

		switch parts[0] {
		case "payload":
			if len(parts) == 1 {
				return tmplStringify(msg.Payload)
			}
			return tmplNestedValue(msg.Payload, parts[1])
		case "meta":
			if len(parts) > 1 {
				if v, ok := msg.GetMeta(parts[1]); ok {
					return tmplStringify(v)
				}
			}
			return ""
		case "topic":
			return msg.Topic
		case "id":
			return msg.ID
		case "timestamp":
			return msg.Timestamp.Format(time.RFC3339)
		default:
			return match
		}
	})
}

// tmplNestedValue resolves a dot-path within a nested map structure.
func tmplNestedValue(data interface{}, path string) string {
	current := data
	for _, part := range strings.Split(path, ".") {
		m, ok := current.(map[string]interface{})
		if !ok {
			return ""
		}
		current, ok = m[part]
		if !ok {
			return ""
		}
	}
	return tmplStringify(current)
}

// tmplStringify renders a value as a string (JSON for composites).
func tmplStringify(v interface{}) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	default:
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", v)
	}
}

func init() {
	info := TemplateProcessInfo()
	if err := node.Register(info, NewTemplateProcess); err != nil {
		panic("failed to register template process: " + err.Error())
	}
}
