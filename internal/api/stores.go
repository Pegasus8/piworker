package api

import (
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/storage"
)

// The storage each handler reads and writes, as interfaces: a handler
// depends on the methods it calls, not on the whole SQLite store, and a test
// can hand it a fake. *storage.SQLiteStore implements them all.

// FlowStore is what the flow routes store.
type FlowStore interface {
	service.MarksStore
	ListFlows(state *flow.FlowState) ([]*flow.Flow, error)
	GetFlow(id string) (*flow.Flow, error)
	CreateFlow(f *flow.Flow) error
	UpdateFlowVersioned(f *flow.Flow, expected int, meta storage.VersionMeta) (int, error)
	DeleteFlow(id string) error
}

// FlowReader reads one stored flow (the node test's flow context).
type FlowReader interface {
	GetFlow(id string) (*flow.Flow, error)
}

// HealthStore tells whether the storage is still open.
type HealthStore interface {
	IsClosed() bool
}

// HistoryReader reads the recorded runs and their node events.
type HistoryReader interface {
	ListFlowRuns(flowID string, limit, offset int) ([]storage.FlowRun, error)
	ListFlowRunEvents(flowID, runID string) ([]storage.NodeEventRecord, error)
}

// AgentStore is what the agent administration routes store: tokens and
// their grants, approvals, the audit and the agent deploy level.
type AgentStore interface {
	AgentDeployLevel() (storage.AgentDeployLevel, error)
	SetAgentDeployLevel(level storage.AgentDeployLevel) error
	CreateAPIToken(req storage.NewAPIToken, now time.Time) (*storage.APIToken, string, error)
	ListAPITokens() ([]storage.APIToken, error)
	UpdateAPITokenGrants(id string, fn func([]storage.TokenGrant) ([]storage.TokenGrant, error)) (*storage.APIToken, error)
	GetAgentApproval(id string) (*storage.AgentApproval, error)
	ListAgentApprovals(f storage.AgentApprovalFilter) ([]storage.AgentApproval, error)
	CountPendingAgentApprovals(now time.Time) (int, error)
	DecideAgentApproval(id string, approve bool, decidedBy string, now time.Time) (*storage.AgentApproval, error)
	ExpireAgentApprovals(now time.Time) (int, error)
	CountPendingFlowDrafts() (int, error)
	ListMCPAudit(f storage.MCPAuditFilter) ([]storage.MCPAuditEntry, error)
	ListMCPRejections(f storage.MCPAuditFilter) ([]storage.MCPRejection, error)
	MCPRejectionTotals(f storage.MCPRejectionFilter) ([]storage.MCPRejectionTotal, bool, error)
}

var (
	_ FlowStore     = (*storage.SQLiteStore)(nil)
	_ HealthStore   = (*storage.SQLiteStore)(nil)
	_ HistoryReader = (*storage.SQLiteStore)(nil)
	_ AgentStore    = (*storage.SQLiteStore)(nil)
)

// The AI memory routes read the in-memory LLM stores (they write through to
// SQLite): *llm.Memory and *llm.Knowledge implement these.

// LLMConversations is the conversation memory (ADR-0038) the editor lists,
// reads and clears.
type LLMConversations interface {
	List() []llm.ConversationInfo
	Turns(key string) []llm.Turn
	Clear(key string) error
}

// KnowledgeBase is the knowledge base (ADR-0038) the editor browses and
// prunes.
type KnowledgeBase interface {
	Collections() []llm.CollectionInfo
	Collection(name string) (llm.CollectionInfo, bool)
	Documents(collection string, offset, limit int) ([]llm.Document, int)
	Delete(collection, id string) error
	DeleteCollection(collection string) error
}

var (
	_ LLMConversations = (*llm.Memory)(nil)
	_ KnowledgeBase    = (*llm.Knowledge)(nil)
)
