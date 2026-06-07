package builtin

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
)

// FileAction writes the (templated) message content to a file.
type FileAction struct {
	*node.BaseNode

	path    string
	content string
	mode    string // "append" or "overwrite"
	newline bool
}

// NewFileAction creates a new FileAction from configuration.
func NewFileAction(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	path := base.GetConfigString("path", "")
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	mode := base.GetConfigString("mode", "append")
	if mode != "append" && mode != "overwrite" {
		mode = "append"
	}

	return &FileAction{
		BaseNode: base,
		path:     path,
		content:  base.GetConfigString("content", "{{payload}}"),
		mode:     mode,
		newline:  base.GetConfigBool("newline", true),
	}, nil
}

// Process renders the content template and writes it to the file.
func (a *FileAction) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	data := renderTemplate(a.content, msg)
	if a.newline && !strings.HasSuffix(data, "\n") {
		data += "\n"
	}

	var err error
	if a.mode == "overwrite" {
		err = os.WriteFile(a.path, []byte(data), 0o644)
	} else {
		f, openErr := os.OpenFile(a.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if openErr != nil {
			return nil, fmt.Errorf("failed to open file: %w", openErr)
		}
		defer f.Close()
		_, err = f.WriteString(data)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	out := msg.Clone()
	out.SourcePort = "output"
	out.SetMeta("fileWritten", a.path)
	out.SetMeta("bytesWritten", len(data))
	return []*types.Message{out}, nil
}

// Ports returns the port definitions.
func (a *FileAction) Ports() (inputs []types.Port, outputs []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		[]types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}}
}

// Validate checks the configuration.
func (a *FileAction) Validate() error {
	if a.path == "" {
		return fmt.Errorf("path is required")
	}
	return nil
}

func fileActionConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"path": {
				Type:        "string",
				Title:       "File Path",
				Description: "Absolute or relative path of the file to write",
			},
			"content": {
				Type:        "string",
				Title:       "Content",
				Description: "Content to write (supports {{payload.field}} templates)",
				Default:     "{{payload}}",
			},
			"mode": {
				Type:        "string",
				Title:       "Mode",
				Description: "append to the file or overwrite it",
				Default:     "append",
				Enum:        []string{"append", "overwrite"},
			},
			"newline": {
				Type:        "boolean",
				Title:       "Append Newline",
				Description: "Append a trailing newline if missing",
				Default:     true,
			},
		},
		Required: []string{"path"},
	}
}

// GetConfigSchema returns the configuration schema for UI.
func (a *FileAction) GetConfigSchema() node.ConfigSchema {
	return fileActionConfigSchema()
}

// FileActionInfo returns the node type info for registration.
func FileActionInfo() node.NodeTypeInfo {
	schema := fileActionConfigSchema()
	return node.NodeTypeInfo{
		Type:        "action-file",
		Name:        "Write File",
		Description: "Write or append the message content to a file",
		Documentation: `## Write File

Writes the (optionally templated) content to a file on the host. Handy for
logging events to disk, building CSV files, or persisting state.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| path | string | - | File path (required) |
| content | string | {{payload}} | Content to write (supports templates) |
| mode | enum | append | append or overwrite |
| newline | boolean | true | Append a trailing newline |

## Output Metadata

- **meta.fileWritten** - The path written
- **meta.bytesWritten** - Number of bytes written
`,
		Category: types.NodeCategoryOutput,
		Inputs:   []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		Outputs:  []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}},
		Config:   &schema,
		Icon:     "file-text",
	}
}

func init() {
	info := FileActionInfo()
	if err := node.Register(info, NewFileAction); err != nil {
		panic("failed to register file action: " + err.Error())
	}
}
