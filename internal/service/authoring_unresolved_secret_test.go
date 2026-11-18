package service

import (
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An optional credential the agent writes with an undefined secret is one
// warning: the authoring unknown_secret, which names what the agent wrote
// (Lint's unresolved_secret of the same setting is left out). A setting it
// did not write keeps Lint's warning.
func TestValidateReportsUnresolvedOptionalSecretOnce(t *testing.T) {
	e := newAuthEnv(t)
	agent := e.token()
	f := safeFlow()
	addNode(f, "mail", "action-email", map[string]any{"host": "smtp.example.com", "from": "a@example.com",
		"to": "b@example.com", "password": "{{secret.smtp_pass}}"})
	connect(f, "calc", "mail")

	res, err := e.a.Validate(t.Context(), agent, ValidateRequest{Flow: f})
	require.NoError(t, err)
	assert.True(t, res.Valid, "a warning does not block")
	codes := issueCodes(res.Issues)
	assert.Contains(t, codes, IssueUnknownSecret)
	assert.NotContains(t, codes, flow.IssueUnresolvedSecret, "one warning per setting")
	assert.Contains(t, issueAt(t, res.Issues, "/nodes/mail/config/password"), "smtp_pass")
}

// The admin's optional credential, which the agent cannot read, keeps the
// name of its undefined secret from the agent: the warning says only that
// a setting it cannot read references one.
func TestUnresolvedSecretOfAHiddenSettingIsNotNamed(t *testing.T) {
	e := newAuthEnv(t)
	f := safeFlow()
	addNode(f, "mail", "action-email", map[string]any{"host": "smtp.example.com", "from": "a@example.com",
		"to": "b@example.com", "password": "{{secret.zzadmin_onlyzz}}"})
	connect(f, "calc", "mail")
	stored := e.create(f)
	agent := e.token()

	res, err := e.a.Validate(t.Context(), agent, ValidateRequest{FlowID: stored.ID})
	require.NoError(t, err)
	assert.Contains(t, issueCodes(res.Issues), flow.IssueUnresolvedSecret)
	assert.NotContains(t, mustJSON(t, res.Issues), "zzadmin_onlyzz")
	assert.Contains(t, issueAt(t, res.Issues, "/nodes/mail/config/password"), "cannot read references a secret that is not defined")
}
