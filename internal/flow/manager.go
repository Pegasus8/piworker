package flow

import (
	"context"
	"fmt"
	"sync"

	"github.com/Pegasus8/piworker/internal/metrics"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/rs/zerolog"
)

// RuntimeManager manages multiple flow runtimes.
type RuntimeManager struct {
	runtimes map[string]*FlowRuntime
	// stopping holds undeployed runtimes whose Stop timed out and that are
	// still draining; their flow ID cannot be deployed again until they finish.
	stopping    map[string]*FlowRuntime
	registry    *node.Registry
	logger      zerolog.Logger
	observer    NodeEventHook
	runtimeOpts []RuntimeOption
	// toolCaller (guarded by mu) is given to every flow deployed after
	// SetToolCaller; nil: none.
	toolCaller node.ToolCaller
	mu         sync.RWMutex
	// toolsVersion (guarded by mu) changes whenever the set of running flows,
	// and so the published tools, may have changed (see Tools).
	toolsVersion uint64
}

// ManagerOption configures a RuntimeManager.
type ManagerOption func(*RuntimeManager)

// WithManagerObserver sets a NodeEventHook applied to every flow this manager
// deploys, so the whole process shares one observability sink.
func WithManagerObserver(hook NodeEventHook) ManagerOption {
	return func(m *RuntimeManager) {
		m.observer = hook
	}
}

// WithRuntimeOptions applies extra RuntimeOptions to every flow this manager
// deploys.
func WithRuntimeOptions(opts ...RuntimeOption) ManagerOption {
	return func(m *RuntimeManager) {
		m.runtimeOpts = append(m.runtimeOpts, opts...)
	}
}

// SetToolCaller gives every flow deployed from now on tc as its nodes' tool
// caller (WithToolCaller; nil: none). The composition root sets it once the
// tool service exists, which needs the manager first, and before any flow
// is deployed or restored; flows already running keep the caller they were
// deployed with.
func (m *RuntimeManager) SetToolCaller(tc node.ToolCaller) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolCaller = tc
}

// NewRuntimeManager creates a new RuntimeManager.
func NewRuntimeManager(registry *node.Registry, logger zerolog.Logger, opts ...ManagerOption) *RuntimeManager {
	m := &RuntimeManager{
		runtimes: make(map[string]*FlowRuntime),
		stopping: make(map[string]*FlowRuntime),
		registry: registry,
		logger:   logger,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Deploy creates and starts a runtime for the given flow. A node config that
// does not match its schema (Flow.CheckConfig) fails the deploy.
func (m *RuntimeManager) Deploy(ctx context.Context, f *Flow) error {
	return m.deploy(ctx, f)
}

// Restore redeploys a flow that was running before the process restarted.
// Unlike Deploy it does not refuse node configs that fail the schema check:
// it returns those issues for the caller to log and starts the flow anyway,
// with the nodes clamping or defaulting such values exactly as they did
// before the check existed. A flow that ran before an upgrade therefore keeps
// running after it, while the next explicit Deploy asks for a fix. Any other
// error (unknown type, a node that cannot be created or validated) still
// fails the restore.
func (m *RuntimeManager) Restore(ctx context.Context, f *Flow) ([]node.ConfigIssue, error) {
	return f.ConfigIssues(m.registry), m.deploy(ctx, f, withLenientConfig())
}

func (m *RuntimeManager) deploy(ctx context.Context, f *Flow, extra ...RuntimeOption) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already running
	if rt, exists := m.runtimes[f.ID]; exists && rt.IsRunning() {
		return ErrFlowAlreadyRunning
	}

	// Refuse to start a second instance while a previous one is still
	// executing after a Stop that timed out.
	if err := m.checkDrained(f.ID); err != nil {
		return err
	}

	// Create the runtime, forwarding the process-wide observer if one is set.
	rtOpts := []RuntimeOption{WithLogger(m.logger)}
	rtOpts = append(rtOpts, m.runtimeOpts...)
	if m.observer != nil {
		rtOpts = append(rtOpts, WithObserver(m.observer))
	}
	if m.toolCaller != nil {
		rtOpts = append(rtOpts, WithToolCaller(m.toolCaller))
	}
	rtOpts = append(rtOpts, extra...)
	rt, err := NewFlowRuntime(f, m.registry, rtOpts...)
	if err != nil {
		metrics.RecordFlowDeployment(false)
		return err
	}

	// Validate against the ports supplied by the instantiated nodes, not a
	// possibly missing or stale cache in an imported/editor definition, then
	// the tool risk analysis (ADR-0013): security checks, never lenient.
	// These are Lint's collectors (ADR-0007): a flow Lint finds no error in
	// passes them. A refused flow's instances never run and are closed.
	tools, issues, err := validateDeployable(rt, m.registry)
	if err != nil {
		rt.closeNodes()
		metrics.RecordFlowDeployment(false)
		return err
	}
	// Settings that cannot work without their secrets (a bot token, a DSN)
	// fail the deploy while one is missing, not every execution.
	if err := checkRequiredSecrets(ctx, rt); err != nil {
		rt.closeNodes()
		metrics.RecordFlowDeployment(false)
		return err
	}
	rt.warnings = deployWarnings(ctx, rt, m.registry, issues)
	m.keepTools(rt, tools, issues)

	// Start the runtime
	if err := rt.Start(ctx); err != nil {
		metrics.RecordFlowDeployment(false)
		return err
	}

	m.runtimes[f.ID] = rt
	m.toolsVersion++

	// Record metrics
	metrics.RecordFlowDeployment(true)
	metrics.IncrementFlowsRunning()

	m.logger.Info().
		Str("flowID", f.ID).
		Str("flowName", f.Name).
		Msg("Flow deployed successfully")

	return nil
}

// Undeploy stops and removes the runtime for the given flow.
func (m *RuntimeManager) Undeploy(flowID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	rt, exists := m.runtimes[flowID]
	if !exists {
		return ErrFlowNotFound
	}

	wasRunning := rt.IsRunning()
	if wasRunning {
		if err := rt.Stop(); err != nil {
			return err
		}
		// Decrement running flows metric
		metrics.DecrementFlowsRunning()
	}

	delete(m.runtimes, flowID)
	m.toolsVersion++
	if !rt.Drained() {
		m.stopping[flowID] = rt
	}

	m.logger.Info().
		Str("flowID", flowID).
		Msg("Flow undeployed successfully")

	return nil
}

// checkDrained returns ErrFlowStillStopping if a stopped instance of flowID is
// still draining. Callers must hold m.mu.
func (m *RuntimeManager) checkDrained(flowID string) error {
	for _, rt := range []*FlowRuntime{m.stopping[flowID], m.runtimes[flowID]} {
		if rt == nil || rt.Drained() {
			continue
		}
		m.logger.Warn().
			Str("flowID", flowID).
			Msg("Refusing to deploy flow: previous instance is still stopping")
		return fmt.Errorf("%w: flow %s", ErrFlowStillStopping, flowID)
	}
	delete(m.stopping, flowID)
	return nil
}

// GetRuntime returns the runtime for the given flow ID.
func (m *RuntimeManager) GetRuntime(flowID string) (*FlowRuntime, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rt, exists := m.runtimes[flowID]
	return rt, exists
}

// ListRunning returns a list of all running flow IDs.
func (m *RuntimeManager) ListRunning() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	running := make([]string, 0)
	for id, rt := range m.runtimes {
		if rt.IsRunning() {
			running = append(running, id)
		}
	}
	return running
}

// StopAll stops all running flows.
func (m *RuntimeManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, rt := range m.runtimes {
		if rt.IsRunning() {
			if err := rt.Stop(); err != nil {
				m.logger.Error().
					Err(err).
					Str("flowID", id).
					Msg("Error stopping flow")
				continue
			}
			metrics.DecrementFlowsRunning()
		}
	}
	m.toolsVersion++

	m.logger.Info().Msg("All flows stopped")
}
