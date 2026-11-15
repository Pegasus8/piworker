package service

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The lifecycle path (docs/design/mcp.md §10, ADR-0034): every deploy and
// stop, the editor's and the agents', goes through Lifecycle under one lock.

var _ FlowLifecycle = (*Lifecycle)(nil)

type lifeNode struct{}

func (lifeNode) Process(context.Context, *types.Message) ([]*types.Message, error) { return nil, nil }
func (lifeNode) Ports() ([]types.Port, []types.Port) {
	return nil, []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny}}
}
func (lifeNode) Validate() error                                    { return nil }
func (lifeNode) Start(context.Context, chan<- *types.Message) error { return nil }
func (lifeNode) Stop() error                                        { return nil }

// lifecycleEnv is a Lifecycle over a real store and runtime manager with a
// "mock-trigger" node type.
func lifecycleEnv(t *testing.T) (*Lifecycle, *storage.SQLiteStore) {
	t.Helper()
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	registry := node.NewRegistry()
	require.NoError(t, registry.Register(node.NodeTypeInfo{
		Type: "mock-trigger", Name: "Mock Trigger", Category: flow.NodeCategoryInput,
		Outputs: []flow.Port{{ID: "output", Name: "Output", DataType: flow.DataTypeAny}},
	}, func(map[string]interface{}) (node.Node, error) { return lifeNode{}, nil }))
	manager := flow.NewRuntimeManager(registry, zerolog.Nop())
	t.Cleanup(manager.StopAll)
	return NewLifecycle(store, manager, zerolog.Nop()), store
}

func createTestFlow(name string) *flow.Flow {
	f := flow.NewFlow(name)
	trigger := flow.NewNode("mock-trigger", flow.NodeCategoryInput)
	trigger.Outputs = []flow.Port{{ID: "output", Name: "Output", DataType: flow.DataTypeAny}}
	f.AddNode(*trigger)
	return f
}

var agentDeployer = DeployActor{Kind: ActorAgent, TokenID: "tok-1", TokenName: "claude"}

// lifecycleCall runs fn under the handler's lifecycle lock.
func lifecycleCall(t *testing.T, h *Lifecycle, fn func(LockedLifecycle) error) error {
	t.Helper()
	return h.WithLock(context.Background(), fn)
}

func requireLifecycleStatus(t *testing.T, err error, status int) *LifecycleError {
	t.Helper()
	var le *LifecycleError
	require.True(t, errors.As(err, &le), "%v is not a *LifecycleError", err)
	assert.Equal(t, status, le.Status, le.Message)
	return le
}

func TestWithLockHoldsTheLifecycleLock(t *testing.T) {
	h, _ := lifecycleEnv(t)
	sentinel := errors.New("from fn")
	err := lifecycleCall(t, h, func(LockedLifecycle) error {
		assert.False(t, h.mu.TryLock(), "fn runs holding lifecycleMu")
		return sentinel
	})
	assert.ErrorIs(t, err, sentinel, "WithLock returns fn's error")
	require.True(t, h.mu.TryLock(), "released afterwards")
	h.mu.Unlock()
}

func TestLockedDeployRecordsTheDeployment(t *testing.T) {
	h, store := lifecycleEnv(t)
	f := createTestFlow("agent deploy")
	require.NoError(t, store.CreateFlow(f))
	require.NoError(t, store.UpdateFlow(f)) // version 2

	var version int
	require.NoError(t, lifecycleCall(t, h, func(l LockedLifecycle) error {
		assert.False(t, l.Running(f.ID))
		var err error
		version, err = l.Deploy(context.Background(), f.ID, agentDeployer, false)
		assert.True(t, l.Running(f.ID))
		return err
	}))
	assert.Equal(t, 2, version)
	// The runtime runs the stored version and reports it with every run
	// (ADR-0022), whatever the marker says.
	rt, ok := h.manager.GetRuntime(f.ID)
	require.True(t, ok)
	assert.Equal(t, 2, rt.Flow().Version)
	stored, err := store.GetFlow(f.ID)
	require.NoError(t, err)
	assert.Equal(t, flow.FlowStateRunning, stored.State, "the restart intent is persisted")
	d, err := store.GetDeployment(f.ID)
	require.NoError(t, err)
	require.NotNil(t, d)
	assert.Equal(t, 2, d.Version)
	assert.Equal(t, storage.VersionActorAgent, d.ActorKind)
	assert.Equal(t, "tok-1", d.TokenID)
	assert.Equal(t, "claude", d.TokenName)

	// Deploying a running flow conflicts unless it restarts it.
	err = lifecycleCall(t, h, func(l LockedLifecycle) error {
		_, err := l.Deploy(context.Background(), f.ID, agentDeployer, false)
		return err
	})
	requireLifecycleStatus(t, err, http.StatusConflict)

	// A restart deploys the stored current version.
	f.Name = "agent deploy v3"
	require.NoError(t, store.UpdateFlow(f))
	require.NoError(t, lifecycleCall(t, h, func(l LockedLifecycle) error {
		var err error
		version, err = l.Deploy(context.Background(), f.ID, DeployActor{Kind: ActorEditor}, true)
		return err
	}))
	assert.Equal(t, 3, version)
	d, err = store.GetDeployment(f.ID)
	require.NoError(t, err)
	assert.Equal(t, 3, d.Version)
	assert.Equal(t, storage.VersionActorEditor, d.ActorKind)
	assert.Empty(t, d.TokenID)
	rt, ok = h.manager.GetRuntime(f.ID)
	require.True(t, ok)
	assert.True(t, rt.IsRunning())
	assert.Equal(t, 3, rt.Flow().Version)
}

func TestLockedDeployRefusals(t *testing.T) {
	h, store := lifecycleEnv(t)
	deploy := func(id string) error {
		return lifecycleCall(t, h, func(l LockedLifecycle) error {
			_, err := l.Deploy(context.Background(), id, agentDeployer, false)
			return err
		})
	}
	requireLifecycleStatus(t, deploy("nope"), http.StatusNotFound)

	bad := createTestFlow("invalid")
	bad.AddNode(*flow.NewNode("no-such-type", flow.NodeCategoryProcessing))
	require.NoError(t, store.CreateFlow(bad))
	le := requireLifecycleStatus(t, deploy(bad.ID), http.StatusBadRequest)
	assert.ErrorIs(t, le, flow.ErrInvalidNodeConfig, "the cause is deploy's error")
	d, err := store.GetDeployment(bad.ID)
	require.NoError(t, err)
	assert.Nil(t, d, "a refused deploy records nothing")

	ok := createTestFlow("fine")
	require.NoError(t, store.CreateFlow(ok))
	h.BeginShutdown()
	requireLifecycleStatus(t, deploy(ok.ID), http.StatusServiceUnavailable)
}

// A redeploy that fails after the old instance stopped leaves the flow
// stopped with its running intent, which the read views show as failed.
func TestLockedRedeployFailureKeepsTheRunningIntent(t *testing.T) {
	h, store := lifecycleEnv(t)
	f := createTestFlow("redeploy")
	require.NoError(t, store.CreateFlow(f))
	require.NoError(t, lifecycleCall(t, h, func(l LockedLifecycle) error {
		_, err := l.Deploy(context.Background(), f.ID, agentDeployer, false)
		return err
	}))
	f.AddNode(*flow.NewNode("no-such-type", flow.NodeCategoryProcessing))
	require.NoError(t, store.UpdateFlow(f))
	err := lifecycleCall(t, h, func(l LockedLifecycle) error {
		_, err := l.Deploy(context.Background(), f.ID, agentDeployer, true)
		return err
	})
	requireLifecycleStatus(t, err, http.StatusBadRequest)
	_, running := h.manager.GetRuntime(f.ID)
	assert.False(t, running)
	stored, err := store.GetFlow(f.ID)
	require.NoError(t, err)
	assert.Equal(t, flow.FlowStateRunning, stored.State)
	d, err := store.GetDeployment(f.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, d.Version, "the marker names the last successful deployment")
}

func TestLockedStop(t *testing.T) {
	h, store := lifecycleEnv(t)
	f := createTestFlow("stop me")
	require.NoError(t, store.CreateFlow(f))
	stop := func(id string) error {
		return lifecycleCall(t, h, func(l LockedLifecycle) error {
			return l.Stop(context.Background(), id, agentDeployer)
		})
	}
	requireLifecycleStatus(t, stop(f.ID), http.StatusConflict)
	requireLifecycleStatus(t, stop("nope"), http.StatusNotFound)

	require.NoError(t, lifecycleCall(t, h, func(l LockedLifecycle) error {
		_, err := l.Deploy(context.Background(), f.ID, agentDeployer, false)
		return err
	}))
	h.BeginShutdown() // stopping stays allowed
	require.NoError(t, stop(f.ID))
	_, running := h.manager.GetRuntime(f.ID)
	assert.False(t, running)
	stored, err := store.GetFlow(f.ID)
	require.NoError(t, err)
	assert.Equal(t, flow.FlowStateInactive, stored.State)
	d, err := store.GetDeployment(f.ID)
	require.NoError(t, err)
	require.NotNil(t, d, "the marker names the last deployment")
	assert.Equal(t, storage.VersionActorAgent, d.ActorKind)
}

// Running reads without the lifecycle lock: it answers while the lock is
// held (a slow restart, a drain timeout).
func TestRunningReadsWithoutTheLock(t *testing.T) {
	h, store := lifecycleEnv(t)
	f := createTestFlow("running")
	require.NoError(t, store.CreateFlow(f))
	assert.False(t, h.Running(f.ID))
	require.NoError(t, lifecycleCall(t, h, func(l LockedLifecycle) error {
		_, err := l.Deploy(context.Background(), f.ID, agentDeployer, false)
		assert.True(t, h.Running(f.ID), "while the lock is held")
		return err
	}))
	assert.True(t, h.Running(f.ID))
}

// failDeploymentMarkers makes every RecordDeployment fail (a SQLite trigger:
// the handler's store is the real one).
func failDeploymentMarkers(t *testing.T, store *storage.SQLiteStore) {
	t.Helper()
	_, err := store.DB().Exec(`
		CREATE TRIGGER fail_marker_insert BEFORE INSERT ON flow_deployments BEGIN SELECT RAISE(ABORT, 'marker refused'); END;
		CREATE TRIGGER fail_marker_update BEFORE UPDATE ON flow_deployments BEGIN SELECT RAISE(ABORT, 'marker refused'); END;`)
	require.NoError(t, err)
}

// A deployment whose marker cannot be recorded is undone, so what runs is
// always attributed: a first deploy ends stopped with its old state, a
// restart stopped with its running intent (read views: failed); 500 either
// way.
func TestDeployUndoneWhenTheMarkerFails(t *testing.T) {
	t.Run("first deploy", func(t *testing.T) {
		h, store := lifecycleEnv(t)
		f := createTestFlow("unmarked")
		require.NoError(t, store.CreateFlow(f))
		failDeploymentMarkers(t, store)

		err := lifecycleCall(t, h, func(l LockedLifecycle) error {
			_, err := l.Deploy(context.Background(), f.ID, agentDeployer, false)
			return err
		})
		le := requireLifecycleStatus(t, err, http.StatusInternalServerError)
		assert.Nil(t, le.ToolError(), "an internal failure, not a refusal")
		assert.False(t, h.Running(f.ID))
		stored, err := store.GetFlow(f.ID)
		require.NoError(t, err)
		assert.Equal(t, flow.FlowStateInactive, stored.State, "the old state is restored")

		d, err := store.GetDeployment(f.ID)
		require.NoError(t, err)
		assert.Nil(t, d)
	})
	t.Run("restart", func(t *testing.T) {
		h, store := lifecycleEnv(t)
		f := createTestFlow("unmarked restart")
		require.NoError(t, store.CreateFlow(f))
		require.NoError(t, lifecycleCall(t, h, func(l LockedLifecycle) error {
			_, err := l.Deploy(context.Background(), f.ID, agentDeployer, false)
			return err
		}))
		f.Name = "v2"
		require.NoError(t, store.UpdateFlow(f))
		failDeploymentMarkers(t, store)

		err := lifecycleCall(t, h, func(l LockedLifecycle) error {
			_, err := l.Deploy(context.Background(), f.ID, agentDeployer, true)
			return err
		})
		requireLifecycleStatus(t, err, http.StatusInternalServerError)
		assert.False(t, h.Running(f.ID), "the unattributed instance is stopped")
		stored, err := store.GetFlow(f.ID)
		require.NoError(t, err)
		assert.Equal(t, flow.FlowStateRunning, stored.State, "the running intent is kept")
		d, err := store.GetDeployment(f.ID)
		require.NoError(t, err)
		assert.Equal(t, 1, d.Version, "the marker still names the last recorded deployment")
	})
}

// The editor's deploy (Start) refuses a version the client did not load and
// says which one is stored; toggling on a running flow only keeps the intent.
func TestStartChecksTheExpectedVersion(t *testing.T) {
	h, store := lifecycleEnv(t)
	f := createTestFlow("versions")
	require.NoError(t, store.CreateFlow(f))
	require.NoError(t, store.UpdateFlow(f)) // version 2

	held := h.Hold()
	_, le := held.Start(context.Background(), f.ID, StartOptions{Expected: 1, Strict: true, By: EditorActor().DeployActor()})
	require.NotNil(t, le)
	assert.Equal(t, http.StatusConflict, le.Status)
	assert.Equal(t, 2, le.CurrentVersion)
	assert.ErrorIs(t, le, storage.ErrVersionConflict)

	got, le := held.Start(context.Background(), f.ID, StartOptions{Expected: 2, Strict: true, By: EditorActor().DeployActor()})
	require.Nil(t, le)
	assert.Equal(t, 2, got.Version)
	assert.Equal(t, flow.FlowStateRunning, got.State)

	_, le = held.Start(context.Background(), f.ID, StartOptions{By: EditorActor().DeployActor()})
	assert.Nil(t, le, "not strict: a running flow keeps running")
	_, le = held.Halt(f.ID, false)
	assert.Nil(t, le)
	_, le = held.Halt(f.ID, false)
	assert.Nil(t, le, "not strict: stopping a stopped flow is not a conflict")
	_, le = held.Halt(f.ID, true)
	require.NotNil(t, le)
	assert.Equal(t, http.StatusConflict, le.Status)
	held.Release()
	require.True(t, h.mu.TryLock(), "Release frees the lock")
	h.mu.Unlock()

	_, le = held.Start(context.Background(), f.ID, StartOptions{})
	require.NotNil(t, le, "a released hold refuses to act")
}

// Discard stops a running flow without touching storage (the flow is being
// deleted).
func TestDiscardStopsARunningFlow(t *testing.T) {
	h, store := lifecycleEnv(t)
	f := createTestFlow("discard")
	require.NoError(t, store.CreateFlow(f))
	held := h.Hold()
	defer held.Release()
	_, le := held.Start(context.Background(), f.ID, StartOptions{Strict: true})
	require.Nil(t, le)
	held.Discard(f.ID)
	assert.False(t, h.Running(f.ID))
	held.Discard("nope")
}

// RestoreRunning starts the flows stored as running (startup) and marks the
// ones that cannot start failed.
func TestRestoreRunning(t *testing.T) {
	h, store := lifecycleEnv(t)
	good := createTestFlow("good")
	require.NoError(t, store.CreateFlow(good))
	require.NoError(t, store.UpdateFlowState(good.ID, flow.FlowStateRunning))
	bad := createTestFlow("bad")
	bad.AddNode(*flow.NewNode("no-such-type", flow.NodeCategoryProcessing))
	require.NoError(t, store.CreateFlow(bad))
	require.NoError(t, store.UpdateFlowState(bad.ID, flow.FlowStateRunning))
	idle := createTestFlow("idle")
	require.NoError(t, store.CreateFlow(idle))

	restored, failed := h.RestoreRunning(context.Background())
	assert.Equal(t, 1, restored)
	assert.Equal(t, 1, failed)
	assert.True(t, h.Running(good.ID))
	assert.False(t, h.Running(idle.ID))
	stored, err := store.GetFlow(bad.ID)
	require.NoError(t, err)
	assert.Equal(t, flow.FlowStateFailed, stored.State)

	h.BeginShutdown()
	restored, failed = h.RestoreRunning(context.Background())
	assert.Zero(t, restored+failed, "nothing starts after shutdown began")
}
