package flow_test

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/secrets"
)

// unresolvedSecretFlow sends an email whose SMTP password references a
// secret, an optional credential: the node runs without it.
func unresolvedSecretFlow() *flow.Flow {
	f := flow.NewFlow("unresolved")
	f.Nodes = []flow.Node{
		lintNode("tick", "trigger-interval", map[string]any{"interval": float64(1000)}),
		lintNode("mail", "action-email", map[string]any{"host": "smtp.example.com", "from": "a@example.com",
			"to": "b@example.com", "password": "{{secret.pw_lint_smtp}}"}),
	}
	f.Connections = []flow.Connection{lintConn("c1", "tick", "output", "mail", "input")}
	return f
}

// TestLintWarnsAboutUnresolvedOptionalSecrets: an optional credential that
// references an undefined secret is a warning (it would resolve to an empty
// value), not an error: the flow still deploys, and the runtime keeps the
// same issue as a deploy warning so the editor can show it. Only the name is
// quoted.
func TestLintWarnsAboutUnresolvedOptionalSecrets(t *testing.T) {
	f := unresolvedSecretFlow()
	empty := secrets.WithStore(context.Background(), secrets.NewStore())

	issues := flow.LintContext(empty, f, node.DefaultRegistry)
	require.Equal(t, []string{"warning:" + flow.IssueUnresolvedSecret + "@/nodes/mail/config/password"}, issueKeys(issues))
	assert.Contains(t, issues[0].Message, `"pw_lint_smtp"`)
	assert.Empty(t, flow.BlockingIssues(issues))
	assert.True(t, flow.EnvironmentIssue(issues[0]), "it depends on the secrets defined")

	m := flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop())
	require.NoError(t, m.Deploy(empty, f), "a warning does not stop the deploy")
	rt, ok := m.GetRuntime(f.ID)
	require.True(t, ok)
	warnings := rt.Warnings()
	require.Len(t, warnings, 1)
	assert.Equal(t, issues[0], warnings[0].Issue, "the deploy's warnings are Lint's")
	assert.Equal(t, "pw_lint_smtp", warnings[0].Secret, "the secret is a field, not only words in the message")
	require.NoError(t, m.Undeploy(f.ID))

	store := secrets.NewStore()
	require.NoError(t, store.Set("pw_lint_smtp", "a-long-smtp-password-value"))
	defined := secrets.WithStore(context.Background(), store)
	assert.Empty(t, flow.LintContext(defined, f, node.DefaultRegistry))
	require.NoError(t, m.Deploy(defined, f))
	rt, _ = m.GetRuntime(f.ID)
	assert.NotNil(t, rt.Warnings())
	assert.Empty(t, rt.Warnings())
	require.NoError(t, m.Undeploy(f.ID))
}

// TestLintDoesNotWarnAboutDisabledNodesSecrets: a disabled node does not
// run, so its references are not checked.
func TestLintDoesNotWarnAboutDisabledNodesSecrets(t *testing.T) {
	f := unresolvedSecretFlow()
	f.Nodes[1].Enabled = false
	empty := secrets.WithStore(context.Background(), secrets.NewStore())
	for _, is := range flow.LintContext(empty, f, node.DefaultRegistry) {
		assert.NotEqual(t, flow.IssueUnresolvedSecret, is.Code)
	}
}
