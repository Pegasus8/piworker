package service

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// apiKeyCanary is the value of the secret api_key in authoring tests: it
// must never reach an agent.
const apiKeyCanary = "sk-live-canary-0123456789abcdef"

// authEnv is a SQLite store, a runtime manager, a lifecycle path over them
// (the editor's, internal/api, in production) and the authoring service.
type authEnv struct {
	t       *testing.T
	store   *storage.SQLiteStore
	secrets *secrets.Store
	audit   *AuditWriter
	manager *flow.RuntimeManager
	life    *fakeLifecycle
	a       *Authoring
}

func newAuthEnv(t *testing.T, customize ...func(*AuthoringOptions)) *authEnv {
	t.Helper()
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "authoring.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	e := &authEnv{t: t, store: store, secrets: secrets.NewStore()}
	require.NoError(t, e.secrets.Set("api_key", apiKeyCanary))
	e.audit = NewAuditWriter(store, AuditOptions{FlushInterval: time.Hour})
	e.manager = flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop())
	t.Cleanup(e.manager.StopAll)
	e.life = &fakeLifecycle{store: store, manager: e.manager}
	opts := AuthoringOptions{
		Store: store, Lifecycle: e.life, Registry: node.DefaultRegistry, Secrets: e.secrets,
		Audit: e.audit, AuthorEnabled: true, Logger: zerolog.Nop(),
	}
	for _, c := range customize {
		c(&opts)
	}
	e.a = NewAuthoring(opts)
	return e
}

// token creates an agent token with scopes (author and read by default:
// authoring on stored flows needs both).
func (e *authEnv) token(scopes ...string) Actor {
	e.t.Helper()
	if len(scopes) == 0 {
		scopes = []string{storage.ScopeAuthor, storage.ScopeRead}
	}
	tok, _, err := e.store.CreateAPIToken(storage.NewAPIToken{Name: "agent-" + uuid.NewString()[:8], Scopes: scopes}, time.Now())
	require.NoError(e.t, err)
	return actorFromToken(tok)
}

// create stores f as a new flow (version 1, admin-approved).
func (e *authEnv) create(f *flow.Flow) *flow.Flow {
	e.t.Helper()
	require.NoError(e.t, e.store.CreateFlow(f))
	stored, err := e.store.GetFlow(f.ID)
	require.NoError(e.t, err)
	return stored
}

func (e *authEnv) setLevel(level storage.AgentDeployLevel) {
	e.t.Helper()
	require.NoError(e.t, e.store.SetAgentDeployLevel(level))
}

func (e *authEnv) auditRows() []storage.MCPAuditEntry {
	e.t.Helper()
	require.NoError(e.t, e.audit.Flush())
	rows, err := e.store.ListMCPAudit(storage.MCPAuditFilter{})
	require.NoError(e.t, err)
	return rows
}

func addNode(f *flow.Flow, id, typ string, cfg map[string]any) {
	info, _ := node.DefaultRegistry.Get(typ)
	n := flow.NewNode(typ, info.Category)
	n.ID = id
	if cfg != nil {
		n.Config = cfg
	}
	n.Position = flow.Position{X: float64(240 * len(f.Nodes)), Y: 120}
	f.AddNode(*n)
}

func connect(f *flow.Flow, from, to string) {
	f.AddConnection(*flow.NewConnection(from, "output", to, "input"))
}

// lightsFlow is a manual trigger, a transform and an HTTP call whose
// Authorization header references the api_key secret.
func lightsFlow() *flow.Flow {
	f := flow.NewFlow("lights")
	addNode(f, "start", "trigger-manual", nil)
	addNode(f, "calc", "process-transform", map[string]any{"expression": "payload"})
	addNode(f, "call", "action-http", map[string]any{
		"url": "https://api.example.com/hook/very-private-path", "method": "POST",
		"headers": map[string]any{"Authorization": "{{secret.api_key}}"},
	})
	connect(f, "start", "calc")
	connect(f, "calc", "call")
	return f
}

// safeFlow is a manual trigger and a transform (risk safe).
func safeFlow() *flow.Flow {
	f := flow.NewFlow("safe")
	addNode(f, "start", "trigger-manual", nil)
	addNode(f, "calc", "process-transform", map[string]any{"expression": "payload"})
	connect(f, "start", "calc")
	return f
}

func issueCodes(issues []flow.Issue) []string {
	out := []string{}
	for _, i := range issues {
		out = append(out, i.Code)
	}
	return out
}

func findIssue(issues []flow.Issue, code string) *flow.Issue {
	for i := range issues {
		if issues[i].Code == code {
			return &issues[i]
		}
	}
	return nil
}

func nodeOf(f *flow.Flow, id string) *flow.Node {
	for i := range f.Nodes {
		if f.Nodes[i].ID == id {
			return &f.Nodes[i]
		}
	}
	return nil
}

// fakeLifecycle is the editor's lifecycle path as internal/api implements
// it, over a store and a runtime manager (the real one is tested there and
// end to end in package main).
type fakeLifecycle struct {
	mu       sync.Mutex
	store    *storage.SQLiteStore
	manager  *flow.RuntimeManager
	shutdown bool
	// inLock runs inside WithLock, before fn (tests: a concurrent change).
	inLock func()
	// failDeploy makes the next Deploy fail with it.
	failDeploy *LifecycleError
	deploys    []storage.Deployment
	stops      int
}

func (l *fakeLifecycle) WithLock(ctx context.Context, fn func(LockedLifecycle) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inLock != nil {
		l.inLock()
	}
	return fn(lockedFake{l})
}

type lockedFake struct{ l *fakeLifecycle }

func (f lockedFake) Running(id string) bool {
	rt, ok := f.l.manager.GetRuntime(id)
	return ok && rt.IsRunning()
}

func (f lockedFake) Deploy(ctx context.Context, id string, by DeployActor, restart bool) (int, error) {
	l := f.l
	if l.shutdown {
		return 0, &LifecycleError{Status: http.StatusServiceUnavailable, Message: "PiWorker is shutting down"}
	}
	if le := l.failDeploy; le != nil {
		l.failDeploy = nil
		return 0, le
	}
	fl, err := l.store.GetFlow(id)
	if errors.Is(err, storage.ErrFlowNotFound) {
		return 0, &LifecycleError{Status: http.StatusNotFound, Message: "Flow not found", Err: err}
	}
	if err != nil {
		return 0, err
	}
	if f.Running(id) {
		if !restart {
			return 0, &LifecycleError{Status: http.StatusConflict, Message: "Flow is already running", Err: flow.ErrFlowAlreadyRunning}
		}
		if err := l.manager.Undeploy(id); err != nil {
			return 0, err
		}
	}
	if err := l.manager.Deploy(ctx, fl); err != nil {
		return 0, &LifecycleError{Status: http.StatusBadRequest, Message: "Failed to deploy flow: " + err.Error(), Err: err}
	}
	if err := l.store.UpdateFlowState(id, flow.FlowStateRunning); err != nil {
		return 0, err
	}
	d := by.Deployment(fl.Version)
	if err := l.store.RecordDeployment(id, d); err != nil {
		return 0, err
	}
	l.deploys = append(l.deploys, d)
	return fl.Version, nil
}

func (f lockedFake) Stop(_ context.Context, id string, _ DeployActor) error {
	l := f.l
	if _, err := l.store.GetFlow(id); errors.Is(err, storage.ErrFlowNotFound) {
		return &LifecycleError{Status: http.StatusNotFound, Message: "Flow not found", Err: err}
	}
	if !f.Running(id) {
		return &LifecycleError{Status: http.StatusConflict, Message: "Flow is not running", Err: flow.ErrFlowNotRunning}
	}
	if err := l.store.UpdateFlowState(id, flow.FlowStateInactive); err != nil {
		return err
	}
	l.stops++
	return l.manager.Undeploy(id)
}
