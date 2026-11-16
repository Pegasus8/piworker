package builtin

import (
	"testing"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustKnowledgeSearch(t *testing.T, cfg map[string]any) *KnowledgeSearchProcess {
	t.Helper()
	full := map[string]any{"connection": fakeLLMConnection, "collection": "docs"}
	for k, v := range cfg {
		full[k] = v
	}
	n, err := NewKnowledgeSearchProcess(full)
	require.NoError(t, err)
	require.NoError(t, n.Validate())
	return n.(*KnowledgeSearchProcess)
}

// newFakeQueryEmbedder is a fake server embedding every query as vec,
// spending tokens prompt tokens.
func newFakeQueryEmbedder(t *testing.T, vec []float64, tokens int) *fakeLLM {
	return newFakeLLM(t, func(_ int, path string, body map[string]any) (int, any) {
		if path != "/v1/embeddings" {
			return 404, apiError("unexpected path " + path)
		}
		return 200, embeddingsAnswer(body, func(string) []float64 { return vec }, tokens)
	})
}

// seedKnowledge stores three documents of the embedding model "embed" in
// collection docs: their cosine similarities with [1, 0] are 1, 0.8 and 0.
func seedKnowledge(t *testing.T, kb *llm.Knowledge) {
	t.Helper()
	for _, d := range []llm.Document{
		{ID: "boiler", Text: "Service the boiler in October.", Metadata: map[string]any{"source": "manual"}, Embedding: llm.Embedding{1, 0}},
		{ID: "heating", Text: "The heating runs from 6 to 22.", Embedding: llm.Embedding{0.8, 0.6}},
		{ID: "garden", Text: "Water the garden at dawn.", Embedding: llm.Embedding{0, 1}},
	} {
		d.Model = "embed"
		require.NoError(t, kb.Add("docs", d))
	}
}

func TestKnowledgeSearchInfo(t *testing.T) {
	info := KnowledgeSearchProcessInfo()
	assert.Equal(t, "process-knowledge-search", info.Type)
	assert.Equal(t, types.NodeCategoryProcessing, info.Category)
	assert.Equal(t, node.RiskExternal, info.Risk)
	assert.Equal(t, "book-open", info.Icon)
	require.NotNil(t, info.Config)
	assert.ElementsMatch(t, []string{"connection", "collection", "query"}, info.Config.Required)
	props := info.Config.Properties
	assert.Equal(t, node.OptionsLLMConnections, props["connection"].Options)
	assert.True(t, props["connection"].Destination)
	assert.Equal(t, node.OptionsLLMModels, props["model"].Options)
	assert.Equal(t, node.OptionsKnowledgeCollections, props["collection"].Options)
	assert.Equal(t, "{{payload}}", props["query"].Default)
	assert.Equal(t, 4, props["topK"].Default)
	assert.Equal(t, "integer", props["topK"].Type)
	assert.Equal(t, "number", props["minScore"].Type)
	require.NotNil(t, props["minScore"].Maximum)
	assert.Equal(t, float64(1), *props["minScore"].Maximum)
	assert.Equal(t, 120, props["timeout"].Default)
	for _, visible := range []string{"connection", "model", "collection", "topK", "minScore", "resultField", "timeout"} {
		assert.Truef(t, props[visible].AgentVisible, "%s is AgentVisible", visible)
	}
	assert.False(t, props["query"].AgentVisible)
	assert.Contains(t, info.Documentation, "## Configuration")
	in, out := mustKnowledgeSearch(t, nil).Ports()
	assert.Equal(t, info.Inputs, in)
	assert.Equal(t, info.Outputs, out)
}

func TestKnowledgeSearchRefusesBadSettings(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{"no collection", map[string]any{"collection": nil}, "collection is required"},
		{"bad collection", map[string]any{"collection": "a/b"}, "collection must be"},
		{"blank query", map[string]any{"query": ""}, "query is required"},
		{"topK zero", map[string]any{"topK": 0}, "topK must be between 1 and 50 documents"},
		{"topK large", map[string]any{"topK": 51}, "topK must be between 1 and 50 documents"},
		{"topK not whole", map[string]any{"topK": 2.5}, "topK must be a whole number"},
		{"minScore negative", map[string]any{"minScore": -0.1}, "minScore must be between 0 and 1"},
		{"minScore large", map[string]any{"minScore": 1.5}, "minScore must be between 0 and 1"},
		{"minScore text", map[string]any{"minScore": "high"}, "minScore must be a number"},
		{"resultField", map[string]any{"resultField": "a b"}, "resultField"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := map[string]any{"connection": "c", "collection": "docs"}
			for k, v := range tc.cfg {
				if v == nil {
					delete(cfg, k)
				} else {
					cfg[k] = v
				}
			}
			_, err := NewKnowledgeSearchProcess(cfg)
			assert.ErrorContains(t, err, tc.want)
		})
	}
	n := mustKnowledgeSearch(t, map[string]any{"minScore": 0.5, "topK": 10})
	assert.Equal(t, 0.5, n.minScore)
	assert.Equal(t, 10, n.topK)
	n = mustKnowledgeSearch(t, map[string]any{"minScore": 1})
	assert.Equal(t, 1.0, n.minScore)
}

func TestKnowledgeSearchFindsDocuments(t *testing.T) {
	f := newFakeQueryEmbedder(t, []float64{1, 0}, 6)
	env := f.env(t)
	seedKnowledge(t, env.knowledge)
	n := mustKnowledgeSearch(t, map[string]any{"query": "When? {{payload.question}}", "topK": 2})
	in := types.NewMessage(map[string]any{"question": "boiler service"}, types.DataTypeObject)
	in.SourcePort = "true"
	outs, err := n.Process(env.ctx, in)
	require.NoError(t, err)
	require.Len(t, outs, 1)
	assert.Equal(t, "output", outs[0].SourcePort)
	assert.Equal(t, types.DataTypeObject, outs[0].PayloadType)
	payload := outs[0].Payload.(map[string]any)
	assert.Equal(t, "When? boiler service", payload["query"])
	results := payload["results"].([]any)
	require.Len(t, results, 2)
	first := results[0].(map[string]any)
	assert.Equal(t, "boiler", first["id"])
	assert.Equal(t, "Service the boiler in October.", first["text"])
	assert.InDelta(t, 1.0, first["score"].(float64), 1e-6)
	assert.Equal(t, map[string]any{"source": "manual"}, first["metadata"])
	second := results[1].(map[string]any)
	assert.Equal(t, "heating", second["id"])
	assert.InDelta(t, 0.8, second["score"].(float64), 1e-6)
	assert.Equal(t, map[string]any{}, second["metadata"], "no metadata is an empty object")
	assert.Equal(t, "Service the boiler in October.\n\n---\n\nThe heating runs from 6 to 22.", payload["context"])
	assert.Equal(t, map[string]any{
		"connection": fakeLLMConnection, "model": "embed", "collection": "docs", "inputTokens": int64(6),
	}, outs[0].Meta["knowledge"])

	reqs := f.Requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "embed", reqs[0]["model"], "the collection's model, not the connection's")
	assert.Equal(t, []string{"When? boiler service"}, requestInputs(t, reqs[0]))
	assert.Equal(t, llm.Usage{InputTokens: 6, Requests: 1}, env.store.UsageToday(fakeLLMConnection))

	// The stored metadata is not shared with the message.
	first["metadata"].(map[string]any)["source"] = "changed"
	docs, _ := env.knowledge.Documents("docs", 0, 10)
	for _, d := range docs {
		if d.ID == "boiler" {
			assert.Equal(t, "manual", d.Metadata["source"])
		}
	}
}

func TestKnowledgeSearchMinScoreAndResultField(t *testing.T) {
	f := newFakeQueryEmbedder(t, []float64{1, 0}, 1)
	env := f.env(t)
	seedKnowledge(t, env.knowledge)
	n := mustKnowledgeSearch(t, map[string]any{"minScore": 0.9, "resultField": "kb", "model": "embed"})
	in := types.NewMessage(map[string]any{"text": "boiler"}, types.DataTypeObject)
	outs, err := n.Process(env.ctx, in)
	require.NoError(t, err)
	payload := outs[0].Payload.(map[string]any)
	assert.Equal(t, map[string]any{"text": "boiler"}, in.Payload, "the input is not changed")
	kb := payload["kb"].(map[string]any)
	assert.Equal(t, `{"text":"boiler"}`, kb["query"], "the default query is {{payload}}")
	results := kb["results"].([]any)
	require.Len(t, results, 1, "only the documents scoring at least minScore")
	assert.Equal(t, "boiler", results[0].(map[string]any)["id"])
	assert.Equal(t, "Service the boiler in October.", kb["context"])
	assert.Equal(t, "boiler", payload["text"], "the payload keeps its fields")
}

// An empty collection has nothing to compare with: no tokens are spent.
func TestKnowledgeSearchEmptyCollection(t *testing.T) {
	f := newFakeQueryEmbedder(t, []float64{1, 0}, 1)
	env := f.env(t)
	outs, err := mustKnowledgeSearch(t, nil).Process(env.ctx, types.NewMessage("anything", types.DataTypeString))
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"query": "anything", "results": []any{}, "context": ""}, outs[0].Payload)
	assert.Equal(t, int64(0), outs[0].Meta["knowledge"].(map[string]any)["inputTokens"])
	assert.Empty(t, f.Requests())
}

func TestKnowledgeSearchModelMismatch(t *testing.T) {
	f := newFakeQueryEmbedder(t, []float64{1, 0, 0}, 1)
	env := f.env(t)
	seedKnowledge(t, env.knowledge)

	// Another model: refused before the query is embedded.
	_, err := mustKnowledgeSearch(t, map[string]any{"model": "other-embed"}).Process(env.ctx, types.NewMessage("q", types.DataTypeString))
	require.ErrorIs(t, err, llm.ErrModelMismatch)
	assert.Contains(t, err.Error(), `"embed"`)
	assert.False(t, node.IsRetryable(err))
	assert.Empty(t, f.Requests())

	// The same model name, other dimensions: refused by the search.
	_, err = mustKnowledgeSearch(t, nil).Process(env.ctx, types.NewMessage("q", types.DataTypeString))
	require.ErrorIs(t, err, llm.ErrModelMismatch)
	assert.Len(t, f.Requests(), 1)
}

func TestKnowledgeSearchRefuses(t *testing.T) {
	f := newFakeQueryEmbedder(t, []float64{1, 0}, 1)

	t.Run("an empty query", func(t *testing.T) {
		env := f.env(t)
		seedKnowledge(t, env.knowledge)
		_, err := mustKnowledgeSearch(t, map[string]any{"query": "{{payload.q}}"}).Process(env.ctx, types.NewMessage(map[string]any{}, types.DataTypeObject))
		assert.ErrorContains(t, err, "query is empty")
	})

	t.Run("an Anthropic connection", func(t *testing.T) {
		env := f.env(t, func(c *llm.Connection) { c.Provider = llm.ProviderAnthropic; c.APIKey = "sk-ant-test" })
		seedKnowledge(t, env.knowledge)
		_, err := mustKnowledgeSearch(t, nil).Process(env.ctx, types.NewMessage("q", types.DataTypeString))
		require.ErrorIs(t, err, llm.ErrUnsupported)
		assert.Contains(t, err.Error(), "embeddings")
	})

	assert.Empty(t, f.Requests())
}

func TestKnowledgeCollectionHelpers(t *testing.T) {
	kb := llm.NewKnowledge()
	_, ok := knowledgeCollection(kb, "docs")
	assert.False(t, ok)
	seedKnowledge(t, kb)
	info, ok := knowledgeCollection(kb, "docs")
	require.True(t, ok)
	assert.Equal(t, 3, info.Documents)
	assert.Equal(t, "embed", info.Model)

	assert.NoError(t, checkCollectionModel(kb, "docs", "embed"))
	assert.NoError(t, checkCollectionModel(kb, "new", "anything"), "a new collection takes any model")
	err := checkCollectionModel(kb, "docs", "other")
	require.ErrorIs(t, err, llm.ErrModelMismatch)
	assert.Contains(t, err.Error(), `"embed"`)

	for _, ok := range []string{"docs", "my_docs-2"} {
		name, err := parseKnowledgeCollection(node.NewBaseNode(map[string]any{"collection": ok}))
		assert.NoError(t, err)
		assert.Equal(t, ok, name)
	}
	name, err := parseKnowledgeCollection(node.NewBaseNode(map[string]any{"collection": " docs "}))
	assert.NoError(t, err)
	assert.Equal(t, "docs", name, "trimmed")
	for _, bad := range []any{nil, "", "a b", "{{payload.c}}", 3} {
		_, err := parseKnowledgeCollection(node.NewBaseNode(map[string]any{"collection": bad}))
		assert.Errorf(t, err, "%v", bad)
	}
}
