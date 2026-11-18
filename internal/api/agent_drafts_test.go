package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The editor REST of drafts, versions and rollback (docs/design/mcp.md §10,
// contract D): thin adapters over service.Authoring, under the session and
// CSRF like every editor endpoint.

type draftCall struct {
	method string
	id     string
	query  service.DraftQuery
	deploy bool
	reason string
	limit  int
	actor  service.Actor
}

// fakeDrafts records what the handlers ask the service and answers what the
// test set.
type fakeDrafts struct {
	calls []draftCall
	err   error
}

func (f *fakeDrafts) record(c draftCall) error {
	f.calls = append(f.calls, c)
	return f.err
}

func (f *fakeDrafts) ListDrafts(_ context.Context, actor service.Actor, q service.DraftQuery) (service.DraftList, error) {
	err := f.record(draftCall{method: "list", query: q, actor: actor})
	return service.DraftList{Drafts: []service.DraftSummary{{ID: "d1", TokenName: "claude", CreatedAt: time.Unix(0, 0).UTC()}}}, err
}

func (f *fakeDrafts) GetDraftDetail(_ context.Context, actor service.Actor, id string) (service.DraftDetail, error) {
	err := f.record(draftCall{method: "detail", id: id, actor: actor})
	return service.DraftDetail{DraftSummary: service.DraftSummary{ID: id}, Note: "whole"}, err
}

func (f *fakeDrafts) ApproveDraft(_ context.Context, actor service.Actor, id string, deploy bool) (service.ApplyResult, error) {
	err := f.record(draftCall{method: "approve", id: id, deploy: deploy, actor: actor})
	return service.ApplyResult{FlowID: "f1", Version: 2, Deployed: deploy}, err
}

func (f *fakeDrafts) RejectDraft(_ context.Context, actor service.Actor, id, reason string) error {
	return f.record(draftCall{method: "reject", id: id, reason: reason, actor: actor})
}

func (f *fakeDrafts) Rollback(_ context.Context, actor service.Actor, flowID string) (service.ApplyResult, error) {
	err := f.record(draftCall{method: "rollback", id: flowID, actor: actor})
	return service.ApplyResult{FlowID: flowID, Version: 4}, err
}

func (f *fakeDrafts) Versions(_ context.Context, actor service.Actor, flowID string, limit int) (service.VersionList, error) {
	err := f.record(draftCall{method: "versions", id: flowID, limit: limit, actor: actor})
	return service.VersionList{Current: 3, LastAdminApproved: 1, Versions: []service.VersionEntry{}}, err
}

// draftsEnv is the agent test env with the drafts routes and the request
// guard (CSRF, JSON) the live router installs.
func newDraftsEnv(t *testing.T) (agentTestEnv, *fakeDrafts) {
	t.Helper()
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	svc := &fakeDrafts{}
	NewDraftsHandler(svc, env.agent.logger).RegisterRoutes(env.router)
	env.router.Use(RequestGuardMiddleware(false, nil))
	return env, svc
}

func TestDraftRoutesCallTheServiceAsTheEditor(t *testing.T) {
	env, svc := newDraftsEnv(t)

	w := env.do("GET", "/api/agent/drafts?status=decided&limit=5&beforeId=d9", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	list := data[service.DraftList](t, w)
	require.Len(t, list.Drafts, 1)
	assert.Equal(t, service.DraftQuery{Status: "decided", Limit: 5, BeforeID: "d9"}, svc.calls[0].query)
	assert.Equal(t, service.ActorEditor, svc.calls[0].actor.Kind)

	w = env.do("GET", "/api/agent/drafts/d1", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "whole", data[service.DraftDetail](t, w).Note)

	w = env.do("POST", "/api/agent/drafts/d1/approve", map[string]any{"deploy": true})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, service.ApplyResult{FlowID: "f1", Version: 2, Deployed: true}, data[service.ApplyResult](t, w))
	assert.True(t, svc.calls[2].deploy)

	// No body: approve without deploying.
	r := httptest.NewRequest("POST", "http://piworker.local/api/agent/drafts/d1/approve", nil)
	r.Header.Set("X-PiWorker-Request", "1")
	r.AddCookie(env.cookie)
	w = httptest.NewRecorder()
	env.router.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, svc.calls[3].deploy)

	w = env.do("POST", "/api/agent/drafts/d1/reject", map[string]any{"reason": "not now"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "not now", svc.calls[4].reason)

	w = env.do("GET", "/api/flows/f1/versions?limit=3", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, 3, data[service.VersionList](t, w).Current)
	assert.Equal(t, 3, svc.calls[5].limit)

	w = env.do("POST", "/api/flows/f1/rollback", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, 4, data[service.ApplyResult](t, w).Version)
	assert.Equal(t, "f1", svc.calls[6].id)
}

func TestDraftRoutesRefuseBadRequests(t *testing.T) {
	env, svc := newDraftsEnv(t)
	for _, path := range []string{"/api/agent/drafts?limit=many", "/api/agent/drafts?limit=-1", "/api/flows/f1/versions?limit=x"} {
		w := env.do("GET", path, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code, path)
	}
	w := env.do("POST", "/api/agent/drafts/d1/approve", map[string]any{"deploy": "yes"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w = env.do("POST", "/api/agent/drafts/d1/approve", map[string]any{"deploy": true, "force": true})
	assert.Equal(t, http.StatusBadRequest, w.Code, "unknown members are refused")
	assert.Empty(t, svc.calls)
}

// Writes need the CSRF header and every route the session.
func TestDraftRoutesNeedTheSessionAndCSRF(t *testing.T) {
	env, svc := newDraftsEnv(t)
	for _, path := range []string{"/api/agent/drafts/d1/approve", "/api/agent/drafts/d1/reject", "/api/flows/f1/rollback"} {
		r := httptest.NewRequest("POST", "http://piworker.local"+path, strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(env.cookie)
		w := httptest.NewRecorder()
		env.router.ServeHTTP(w, r)
		assert.Equal(t, http.StatusForbidden, w.Code, "%s without the CSRF header", path)
	}
	for _, path := range []string{"/api/agent/drafts", "/api/agent/drafts/d1", "/api/flows/f1/versions"} {
		w := authRequest(env.router, "GET", path, nil, nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code, path)
	}
	w := authRequest(env.router, "POST", "/api/agent/drafts/d1/approve", map[string]any{}, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Empty(t, svc.calls)
}

// Service refusals keep the editor's statuses and carry their data.
func TestDraftRoutesMapRefusals(t *testing.T) {
	env, svc := newDraftsEnv(t)
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{&service.ToolError{Kind: service.ErrConflict, Message: "stale", CurrentVersion: 3}, http.StatusConflict, "conflict"},
		{&service.ToolError{Kind: service.ErrInvalidArguments, Message: "invalid", FlowIssues: []flow.Issue{{Path: "/nodes/a", Code: "unknown_type", Severity: flow.IssueError, Message: "unknown"}}}, http.StatusBadRequest, "invalid_arguments"},
		{&service.ToolError{Kind: service.ErrToolNotFound, Message: "unknown draft"}, http.StatusNotFound, "not_found"},
		{&service.ToolError{Kind: service.ErrFlowNotFound, Message: "unknown flow"}, http.StatusNotFound, "flow_not_found"},
		{&service.ToolError{Kind: service.ErrUnavailable, Message: "later"}, http.StatusServiceUnavailable, "unavailable"},
	}
	for _, tc := range cases {
		svc.err = tc.err
		w := env.do("POST", "/api/agent/drafts/d1/approve", map[string]any{"deploy": false})
		require.Equal(t, tc.status, w.Code, w.Body.String())
		var body struct {
			Success bool              `json:"success"`
			Error   string            `json:"error"`
			Data    ToolCallErrorData `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.False(t, body.Success)
		assert.Equal(t, tc.code, body.Data.Code)
		assert.Equal(t, tc.err.Error(), body.Error)
		if tc.status == http.StatusConflict {
			assert.Equal(t, 3, body.Data.CurrentVersion)
		}
		if tc.status == http.StatusBadRequest {
			require.Len(t, body.Data.FlowIssues, 1)
		}
	}
	svc.err = context.DeadlineExceeded
	w := env.do("GET", "/api/flows/f1/versions", nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "deadline", "internal failures are not echoed")
}

// The badge's count adds the pending drafts, on the same passive path.
func TestPendingCountIncludesDrafts(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	tok, _, err := env.store.CreateAPIToken(storage.NewAPIToken{Name: "claude", Scopes: []string{storage.ScopeAuthor}}, time.Now())
	require.NoError(t, err)
	for range 2 {
		_, err := env.store.CreateFlowDraft(storage.FlowDraft{Data: []byte(`{"name":"x","nodes":[],"connections":[]}`), AuthorTokenID: tok.ID})
		require.NoError(t, err)
	}
	ageSession(t, env, time.Hour)
	before := sessionLastSeen(t, env)
	w := env.do("GET", PendingApprovalsCountPath, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got := data[PendingApprovalsCount](t, w)
	assert.Equal(t, 0, got.Pending)
	assert.Equal(t, 2, got.Drafts)
	assert.Equal(t, before, sessionLastSeen(t, env), "still passive")
}
