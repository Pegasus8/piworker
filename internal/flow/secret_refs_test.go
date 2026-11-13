package flow_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Pegasus8/piworker/internal/flow"
)

// SecretReferences lists every {{secret.NAME}} written in a node's settings
// (nested values included), in node order, then setting, then name, each
// (node, setting, name) once; a disabled node's are listed and marked.
func TestSecretReferences(t *testing.T) {
	f := flow.NewFlow("refs")
	mail := lintNode("mail", "action-email", map[string]any{"host": "smtp.example.com",
		"username": "{{secret.smtp_user}}", "password": "{{ secret.smtp_pass }}{{secret.smtp_pass}}"})
	call := lintNode("call", "action-http", map[string]any{"url": "https://x.example/?k={{secret.api_key}}",
		"headers": map[string]any{"B": "{{secret.b}}", "A": "Bearer {{secret.a}}"}, "body": "{{payload.x}}"})
	call.Enabled = false
	f.Nodes = []flow.Node{mail, call, lintNode("plain", "trigger-manual", nil)}

	assert.Equal(t, []flow.SecretReference{
		{Name: "smtp_pass", NodeID: "mail", NodeType: "action-email", Setting: "password", Enabled: true},
		{Name: "smtp_user", NodeID: "mail", NodeType: "action-email", Setting: "username", Enabled: true},
		{Name: "a", NodeID: "call", NodeType: "action-http", Setting: "headers", Enabled: false},
		{Name: "b", NodeID: "call", NodeType: "action-http", Setting: "headers", Enabled: false},
		{Name: "api_key", NodeID: "call", NodeType: "action-http", Setting: "url", Enabled: false},
	}, flow.SecretReferences(f))
	assert.Empty(t, flow.SecretReferences(nil))
}
