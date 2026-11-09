package service

import (
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Agent deploy (docs/design/mcp.md §10, contract C3): the agent_deploy level
// decides, the lifecycle path deploys.

func (e *authEnv) deployer() Actor {
	return e.token(storage.ScopeDeploy, storage.ScopeAuthor)
}

// agentVersion saves change applied to the current version of flowID as an
// agent's version (never admin-approved).
func (e *authEnv) agentVersion(flowID string, by Actor, change func(f *flow.Flow)) int {
	e.t.Helper()
	f, err := e.store.GetFlow(flowID)
	require.NoError(e.t, err)
	change(f)
	v, err := e.store.UpdateFlowVersioned(f, 0, storage.VersionMeta{ActorKind: storage.VersionActorAgent, TokenID: by.TokenID, TokenName: by.TokenName})
	require.NoError(e.t, err)
	return v
}

func (e *authEnv) running(flowID string) bool {
	rt, ok := e.manager.GetRuntime(flowID)
	return ok && rt.IsRunning()
}

func TestDeployNeedsTheDeployScopeAndALevel(t *testing.T) {
	e := newAuthEnv(t)
	f := e.create(safeFlow())
	agent := e.deployer()
	_, err := e.a.Deploy(t.Context(), agent, DeployRequest{FlowID: f.ID})
	te := requireKind(t, err, ErrPermissionDenied)
	assert.Contains(t, te.Message, "off")

	e.setLevel(storage.AgentDeployAll)
	_, err = e.a.Deploy(t.Context(), e.token(storage.ScopeAuthor), DeployRequest{FlowID: f.ID})
	requireKind(t, err, ErrPermissionDenied)
	_, err = e.a.Deploy(t.Context(), EditorActor(), DeployRequest{FlowID: f.ID})
	requireKind(t, err, ErrPermissionDenied)
	_, err = e.a.Stop(t.Context(), EditorActor(), f.ID)
	requireKind(t, err, ErrPermissionDenied)
	_, err = e.a.Deploy(t.Context(), agent, DeployRequest{FlowID: "nope"})
	requireKind(t, err, ErrFlowNotFound)
	assert.False(t, e.running(f.ID))
}

// The low level deploys a flow of risk external at most whose changes since
// the version the admin last approved raise none of
// LowDeployRefusedAlerts (H1 included).
func TestLowDeployPolicy(t *testing.T) {
	cases := map[string]struct {
		change func(f *flow.Flow)
		refuse string // an alert kind, or "privileged"; "" allows
	}{
		"a transform changed": {func(f *flow.Flow) { nodeOf(f, "calc").Config["expression"] = "payload * 2" }, ""},
		"no change at all":    {func(*flow.Flow) {}, ""},
		"destination changed": {func(f *flow.Flow) {
			nodeOf(f, "call").Config["url"] = "https://api.example.com/hook/other"
		}, flow.AlertDestinationChanged},
		"dynamic destination": {func(f *flow.Flow) {
			nodeOf(f, "call").Config["url"] = "https://api.example.com/{{payload.where}}"
		}, flow.AlertDynamicDestination},
		"new host": {func(f *flow.Flow) {
			nodeOf(f, "call").Config["url"] = "https://collector.example.net/x"
		}, flow.AlertNewHost},
		"secret added": {func(f *flow.Flow) {
			nodeOf(f, "call").Config["headers"] = map[string]any{"Authorization": "{{secret.api_key}}", "X-Other": "{{secret.other}}"}
		}, flow.AlertSecretAdded},
		"secret moved": {func(f *flow.Flow) {
			addNode(f, "leak", "action-http", map[string]any{"url": "https://api.example.com/hook/very-private-path",
				"headers": map[string]any{"Authorization": "{{secret.api_key}}"}})
			connect(f, "calc", "leak")
		}, flow.AlertSecretMoved},
		"risk switch on": {func(f *flow.Flow) { nodeOf(f, "call").Config["allowPrivate"] = true }, flow.AlertRiskSwitchOn},
		"sensitive literal": {func(f *flow.Flow) {
			nodeOf(f, "call").Config["headers"] = map[string]any{"Authorization": "Bearer typed"}
		}, flow.AlertSensitiveLiteral},
		"credentialed request changed": {func(f *flow.Flow) { nodeOf(f, "call").Config["method"] = "DELETE" }, flow.AlertCredentialedRequestChanged},
		"privileged node added": {func(f *flow.Flow) {
			addNode(f, "cmd", "action-command", map[string]any{"command": "true"})
			connect(f, "calc", "cmd")
		}, flow.AlertPrivilegedNodeAdded},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newAuthEnv(t)
			require.NoError(t, e.secrets.Set("other", "another-secret-value"))
			e.setLevel(storage.AgentDeployLow)
			base := e.create(lightsFlow())
			agent := e.deployer()
			version := e.agentVersion(base.ID, agent, tc.change)
			res, err := e.a.Deploy(t.Context(), agent, DeployRequest{FlowID: base.ID})
			if tc.refuse == "" {
				require.NoError(t, err)
				assert.Equal(t, DeployResult{FlowID: base.ID, Version: version, Running: true, Risk: node.RiskExternal}, res)
				assert.True(t, e.running(base.ID))
				return
			}
			te := requireKind(t, err, ErrPermissionDenied)
			assert.Contains(t, te.Message, tc.refuse)
			assert.NotContains(t, te.Message, "collector.example.net", "kinds, never values")
			assert.False(t, e.running(base.ID), "nothing deployed")
		})
	}
}

func TestLowDeployPolicyRisk(t *testing.T) {
	e := newAuthEnv(t)
	e.setLevel(storage.AgentDeployLow)
	agent := e.deployer()

	// Privileged even when the admin approved it as it is.
	priv := safeFlow()
	addNode(priv, "cmd", "action-command", map[string]any{"command": "true"})
	connect(priv, "calc", "cmd")
	stored := e.create(priv)
	_, err := e.a.Deploy(t.Context(), agent, DeployRequest{FlowID: stored.ID})
	te := requireKind(t, err, ErrPermissionDenied)
	assert.Contains(t, te.Message, "privileged")

	// A flow the admin never approved compares with an empty flow: its
	// destination is a change.
	fresh := lightsFlow()
	require.NoError(t, e.store.CreateFlowVersioned(fresh, storage.VersionMeta{ActorKind: storage.VersionActorAgent, TokenID: agent.TokenID, TokenName: agent.TokenName}))
	_, err = e.a.Deploy(t.Context(), agent, DeployRequest{FlowID: fresh.ID})
	te = requireKind(t, err, ErrPermissionDenied)
	assert.Contains(t, te.Message, flow.AlertDestinationChanged)

	// The same policy guards stopping.
	safe := e.create(safeFlow())
	_, err = e.a.Deploy(t.Context(), agent, DeployRequest{FlowID: safe.ID})
	require.NoError(t, err)
	res, err := e.a.Stop(t.Context(), agent, safe.ID)
	require.NoError(t, err)
	assert.False(t, res.Running)
	assert.False(t, e.running(safe.ID))
	_, err = e.a.Stop(t.Context(), agent, safe.ID)
	requireKind(t, err, ErrConflict)
	require.NoError(t, e.life.WithLock(t.Context(), func(l LockedLifecycle) error {
		_, err := l.Deploy(t.Context(), stored.ID, EditorActor().DeployActor(), false)
		return err
	}))
	_, err = e.a.Stop(t.Context(), agent, stored.ID)
	requireKind(t, err, ErrPermissionDenied)
	assert.True(t, e.running(stored.ID))
}

// The all level deploys any flow, privileged ones included; the audit marks
// such a deploy privileged and the marker names the agent's token.
func TestAllDeployPolicy(t *testing.T) {
	e := newAuthEnv(t)
	e.setLevel(storage.AgentDeployAll)
	agent := e.deployer()
	priv := safeFlow()
	addNode(priv, "cmd", "action-command", map[string]any{"command": "true"})
	connect(priv, "calc", "cmd")
	stored := e.create(priv)
	res, err := e.a.Deploy(t.Context(), agent, DeployRequest{FlowID: stored.ID})
	require.NoError(t, err)
	assert.Equal(t, node.RiskPrivileged, res.Risk)
	assert.True(t, e.running(stored.ID))
	d, err := e.store.GetDeployment(stored.ID)
	require.NoError(t, err)
	assert.Equal(t, storage.VersionActorAgent, d.ActorKind)
	assert.Equal(t, agent.TokenID, d.TokenID)
	assert.Equal(t, agent.TokenName, d.TokenName)

	_, err = e.a.Deploy(t.Context(), agent, DeployRequest{FlowID: stored.ID})
	requireKind(t, err, ErrConflict)

	res, err = e.a.Stop(t.Context(), agent, stored.ID)
	require.NoError(t, err)
	assert.Equal(t, DeployResult{FlowID: stored.ID, Version: 1, Running: false, Risk: node.RiskPrivileged}, res)

	rows := e.auditRows()
	require.Len(t, rows, 3)
	assert.Equal(t, DeployToolStopFlow, rows[0].Tool)
	assert.Equal(t, DeployToolDeployFlow, rows[2].Tool)
	assert.True(t, rows[2].Privileged, "a privileged flow's deploy is never evicted early")
	assert.Equal(t, "completed", rows[2].Outcome)
	assert.Contains(t, rows[2].ArgsPreview, `"version":1`, "the audit names the deployed version")
	assert.Equal(t, "conflict", rows[1].Outcome)
}

// With a draft, the agent's own pending draft is applied as an agent
// version (closing the draft in the same save, H5) and deployed.
func TestDeployADraft(t *testing.T) {
	e := newAuthEnv(t)
	e.setLevel(storage.AgentDeployAll)
	base := e.create(lightsFlow())
	agent := e.deployer()
	saved := e.draft(agent, DraftInput{FlowID: base.ID, BaseVersion: 1, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "calc", Config: map[string]any{"expression": "payload + 1"}},
	}})

	_, err := e.a.Deploy(t.Context(), e.deployer(), DeployRequest{FlowID: base.ID, DraftID: saved.DraftID})
	requireKind(t, err, ErrToolNotFound)
	other := e.create(safeFlow())
	_, err = e.a.Deploy(t.Context(), agent, DeployRequest{FlowID: other.ID, DraftID: saved.DraftID})
	requireKind(t, err, ErrInvalidArguments)

	res, err := e.a.Deploy(t.Context(), agent, DeployRequest{FlowID: base.ID, DraftID: saved.DraftID})
	require.NoError(t, err)
	assert.Equal(t, 2, res.Version)
	assert.True(t, res.Running)
	v, err := e.store.GetFlowVersion(base.ID, 2)
	require.NoError(t, err)
	assert.Equal(t, storage.VersionActorAgent, v.ActorKind)
	assert.False(t, v.AdminApproved)
	assert.Equal(t, saved.DraftID, v.DraftID)
	assert.Equal(t, agent.TokenID, v.TokenID)
	d, err := e.store.GetFlowDraft(saved.DraftID)
	require.NoError(t, err)
	assert.Equal(t, storage.DraftApproved, d.Status)
	assert.Equal(t, 2, d.AppliedVersion)
	marks, err := e.a.Marks(t.Context(), EditorActor(), mustFlow(t, e, base.ID))
	require.NoError(t, err)
	assert.True(t, marks.AgentChanged)
	require.NotNil(t, marks.Deployment)
	assert.Equal(t, "agent", marks.Deployment.ActorKind)
	assert.Equal(t, 2, marks.Deployment.Version)

	// A draft of a running flow restarts it with the new version.
	next := e.draft(agent, DraftInput{FlowID: base.ID, BaseVersion: 2, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "calc", Config: map[string]any{"expression": "payload + 2"}},
	}})
	res, err = e.a.Deploy(t.Context(), agent, DeployRequest{FlowID: base.ID, DraftID: next.DraftID})
	require.NoError(t, err)
	assert.Equal(t, 3, res.Version)
	assert.True(t, e.running(base.ID))

	// A stale draft is a conflict; so is a draft no longer pending.
	stale := e.draft(agent, DraftInput{FlowID: base.ID, BaseVersion: 3, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "calc", Config: map[string]any{"expression": "payload + 3"}},
	}})
	e.agentVersion(base.ID, agent, func(*flow.Flow) {})
	_, err = e.a.Deploy(t.Context(), agent, DeployRequest{FlowID: base.ID, DraftID: stale.DraftID})
	te := requireKind(t, err, ErrConflict)
	assert.Equal(t, 4, te.CurrentVersion)
	_, err = e.a.Deploy(t.Context(), agent, DeployRequest{FlowID: base.ID, DraftID: next.DraftID})
	requireKind(t, err, ErrConflict)
}

func mustFlow(t *testing.T, e *authEnv, id string) *flow.Flow {
	t.Helper()
	f, err := e.store.GetFlow(id)
	require.NoError(t, err)
	return f
}

// Under the lifecycle lock, right before deploying, the token and the
// evaluated version are checked again (H3): nothing is deployed otherwise.
func TestDeployRechecksUnderTheLock(t *testing.T) {
	e := newAuthEnv(t)
	e.setLevel(storage.AgentDeployAll)
	base := e.create(safeFlow())
	agent := e.deployer()

	e.life.inLock = func() { require.NoError(t, e.store.RevokeAPIToken(agent.TokenID, time.Now())) }
	_, err := e.a.Deploy(t.Context(), agent, DeployRequest{FlowID: base.ID})
	requireKind(t, err, ErrPermissionDenied)
	assert.False(t, e.running(base.ID))
	assert.Empty(t, e.life.deploys)

	other := e.deployer()
	e.life.inLock = func() {
		f := mustFlow(t, e, base.ID)
		nodeOf(f, "calc").Config["expression"] = "payload * 9"
		require.NoError(t, e.store.UpdateFlow(f))
	}
	_, err = e.a.Deploy(t.Context(), other, DeployRequest{FlowID: base.ID})
	te := requireKind(t, err, ErrConflict)
	assert.Equal(t, 2, te.CurrentVersion)
	assert.False(t, e.running(base.ID))

	e.life.inLock = func() { require.NoError(t, e.store.SetAgentDeployLevel(storage.AgentDeployOff)) }
	_, err = e.a.Deploy(t.Context(), other, DeployRequest{FlowID: base.ID})
	requireKind(t, err, ErrPermissionDenied)
	assert.Empty(t, e.life.deploys)
}

// The flow is linted first: any Lint error, an undefined secret included,
// refuses the deploy with the issues.
func TestDeployLintsFirst(t *testing.T) {
	e := newAuthEnv(t)
	e.setLevel(storage.AgentDeployAll)
	strict := safeFlow()
	addNode(strict, "tg", "action-telegram", map[string]any{"botToken": "{{secret.bot}}", "chatId": "1"})
	connect(strict, "calc", "tg")
	stored := e.create(strict)
	_, err := e.a.Deploy(t.Context(), e.deployer(), DeployRequest{FlowID: stored.ID})
	te := requireKind(t, err, ErrInvalidArguments)
	assert.Contains(t, issueCodes(te.FlowIssues), flow.IssueUndefinedSecret)
	assert.Empty(t, e.life.deploys)

	// Lifecycle refusals map to the agent's kinds.
	ok := e.create(safeFlow())
	e.life.shutdown = true
	_, err = e.a.Deploy(t.Context(), e.deployer(), DeployRequest{FlowID: ok.ID})
	requireKind(t, err, ErrUnavailable)
}

// Rollback restores the version the admin last approved as a new version,
// redeploying a running flow.
func TestRollback(t *testing.T) {
	e := newAuthEnv(t)
	base := e.create(lightsFlow())
	agent := e.deployer()
	_, err := e.a.Rollback(t.Context(), EditorActor(), base.ID)
	requireKind(t, err, ErrConflict)

	e.agentVersion(base.ID, agent, func(f *flow.Flow) { nodeOf(f, "calc").Config["expression"] = "payload * 7" })
	require.NoError(t, e.life.WithLock(t.Context(), func(l LockedLifecycle) error {
		_, err := l.Deploy(t.Context(), base.ID, agent.DeployActor(), false)
		return err
	}))
	res, err := e.a.Rollback(t.Context(), EditorActor(), base.ID)
	require.NoError(t, err)
	assert.Equal(t, ApplyResult{FlowID: base.ID, Version: 3, Deployed: true}, res)
	restored := mustFlow(t, e, base.ID)
	assert.Equal(t, "payload", nodeOf(restored, "calc").Config["expression"])
	v, err := e.store.GetFlowVersion(base.ID, 3)
	require.NoError(t, err)
	assert.True(t, v.AdminApproved)
	assert.Equal(t, storage.VersionActorEditor, v.ActorKind)
	assert.Equal(t, "rollback to v1", v.Note)
	d, err := e.store.GetDeployment(base.ID)
	require.NoError(t, err)
	assert.Equal(t, 3, d.Version)
	assert.Equal(t, storage.VersionActorEditor, d.ActorKind)

	_, err = e.a.Rollback(t.Context(), EditorActor(), base.ID) // v3 is admin-approved
	requireKind(t, err, ErrConflict)
	_, err = e.a.Rollback(t.Context(), agent, base.ID)
	requireKind(t, err, ErrPermissionDenied)
	_, err = e.a.Rollback(t.Context(), EditorActor(), "nope")
	requireKind(t, err, ErrFlowNotFound)
}

func TestVersionsAndMarks(t *testing.T) {
	e := newAuthEnv(t)
	base := e.create(safeFlow())
	agent := e.deployer()
	e.agentVersion(base.ID, agent, func(f *flow.Flow) { nodeOf(f, "calc").Config["expression"] = "payload * 7" })
	require.NoError(t, e.life.WithLock(t.Context(), func(l LockedLifecycle) error {
		_, err := l.Deploy(t.Context(), base.ID, agent.DeployActor(), false)
		return err
	}))
	list, err := e.a.Versions(t.Context(), EditorActor(), base.ID, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, list.Current)
	assert.Equal(t, 1, list.LastAdminApproved)
	require.Len(t, list.Versions, 2)
	newest := list.Versions[0]
	assert.Equal(t, 2, newest.Version)
	assert.Equal(t, storage.VersionActorAgent, newest.ActorKind)
	assert.Equal(t, agent.TokenName, newest.TokenName)
	assert.False(t, newest.AdminApproved)
	assert.True(t, newest.Deployed)
	assert.False(t, list.Versions[1].Deployed)
	assert.True(t, list.Versions[1].AdminApproved)

	one, err := e.a.Versions(t.Context(), EditorActor(), base.ID, 1)
	require.NoError(t, err)
	assert.Len(t, one.Versions, 1)

	marks, err := e.a.Marks(t.Context(), EditorActor(), mustFlow(t, e, base.ID))
	require.NoError(t, err)
	assert.True(t, marks.AgentChanged)
	require.NotNil(t, marks.Deployment)
	assert.Equal(t, agent.TokenName, marks.Deployment.TokenName)

	_, err = e.a.Versions(t.Context(), agent, base.ID, 0)
	requireKind(t, err, ErrPermissionDenied)
	_, err = e.a.Versions(t.Context(), EditorActor(), "nope", 0)
	requireKind(t, err, ErrFlowNotFound)
	_, err = e.a.Marks(t.Context(), agent, base)
	requireKind(t, err, ErrPermissionDenied)
}

// Without the lifecycle path (internal/api's FlowsHandler) nothing can be
// deployed, stopped or rolled back.
func TestDeployWithoutALifecycleIsUnavailable(t *testing.T) {
	e := newAuthEnv(t, func(o *AuthoringOptions) { o.Lifecycle = nil })
	e.setLevel(storage.AgentDeployAll)
	f := e.create(safeFlow())
	agent := e.deployer()
	_, err := e.a.Deploy(t.Context(), agent, DeployRequest{FlowID: f.ID})
	requireKind(t, err, ErrUnavailable)
	_, err = e.a.Stop(t.Context(), agent, f.ID)
	requireKind(t, err, ErrUnavailable)
	_, err = e.a.Rollback(t.Context(), EditorActor(), f.ID)
	requireKind(t, err, ErrUnavailable)
}

// A refused deploy of a privileged flow is audited as privileged too: the
// row cap never evicts it early.
func TestRefusedPrivilegedDeployIsAuditedPrivileged(t *testing.T) {
	e := newAuthEnv(t)
	e.setLevel(storage.AgentDeployLow)
	priv := safeFlow()
	addNode(priv, "cmd", "action-command", map[string]any{"command": "true"})
	connect(priv, "calc", "cmd")
	stored := e.create(priv)
	_, err := e.a.Deploy(t.Context(), e.deployer(), DeployRequest{FlowID: stored.ID})
	requireKind(t, err, ErrPermissionDenied)
	rows := e.auditRows()
	require.Len(t, rows, 1)
	assert.Equal(t, "forbidden", rows[0].Outcome)
	assert.True(t, rows[0].Privileged)
}
