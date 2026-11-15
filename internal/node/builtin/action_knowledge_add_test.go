package builtin

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// embeddingsAnswer answers an embeddings request: one vector per input,
// vec(input), and tokens prompt tokens.
func embeddingsAnswer(body map[string]any, vec func(string) []float64, tokens int) map[string]any {
	inputs, _ := body["input"].([]any)
	data := make([]any, 0, len(inputs))
	for i, in := range inputs {
		s, _ := in.(string)
		data = append(data, map[string]any{"object": "embedding", "index": i, "embedding": vec(s)})
	}
	return map[string]any{"object": "list", "data": data, "usage": map[string]any{"prompt_tokens": tokens, "total_tokens": tokens}}
}

// lengthVector embeds a text as [1, its length]: distinct, never zero.
func lengthVector(s string) []float64 { return []float64{1, float64(len(s))} }

// newFakeEmbedder is a fake server whose embeddings are lengthVector, each
// request spending tokens prompt tokens.
func newFakeEmbedder(t *testing.T, tokens int) *fakeLLM {
	return newFakeLLM(t, func(_ int, path string, body map[string]any) (int, any) {
		if path != "/v1/embeddings" {
			return 404, apiError("unexpected path " + path)
		}
		return 200, embeddingsAnswer(body, lengthVector, tokens)
	})
}

// requestInputs returns the "input" of an embeddings request.
func requestInputs(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, ok := body["input"].([]any)
	require.True(t, ok, "input: %v", body["input"])
	out := make([]string, len(raw))
	for i, v := range raw {
		out[i] = v.(string)
	}
	return out
}

func mustKnowledgeAdd(t *testing.T, cfg map[string]any) *KnowledgeAddAction {
	t.Helper()
	full := map[string]any{"connection": fakeLLMConnection, "model": "embed", "collection": "docs"}
	for k, v := range cfg {
		full[k] = v
	}
	n, err := NewKnowledgeAddAction(full)
	require.NoError(t, err)
	require.NoError(t, n.Validate())
	return n.(*KnowledgeAddAction)
}

// documentIDs lists the IDs of a collection's documents, sorted.
func documentIDs(kb *llm.Knowledge, collection string) []string {
	docs, _ := kb.Documents(collection, 0, llm.MaxDocuments)
	ids := make([]string, 0, len(docs))
	for _, d := range docs {
		ids = append(ids, d.ID)
	}
	return sortedStrings(ids)
}

func sortedStrings(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

func TestKnowledgeAddInfo(t *testing.T) {
	info := KnowledgeAddActionInfo()
	assert.Equal(t, "action-knowledge-add", info.Type)
	assert.Equal(t, types.NodeCategoryOutput, info.Category)
	assert.Equal(t, node.RiskExternal, info.Risk)
	assert.Equal(t, "book-plus", info.Icon)
	require.NotNil(t, info.Config)
	assert.ElementsMatch(t, []string{"connection", "model", "collection"}, info.Config.Required)
	props := info.Config.Properties
	assert.Equal(t, node.OptionsLLMConnections, props["connection"].Options)
	assert.True(t, props["connection"].Destination)
	assert.Equal(t, node.OptionsLLMModels, props["model"].Options)
	assert.Equal(t, node.OptionsKnowledgeCollections, props["collection"].Options)
	assert.True(t, props["collection"].Destination, "where the text is stored")
	assert.True(t, props["collection"].AgentVisible)
	assert.Equal(t, "{{payload}}", props["text"].Default)
	assert.Equal(t, "object", props["metadata"].Type)
	assert.Equal(t, 0, props["chunkSize"].Default)
	assert.Equal(t, "characters", props["chunkSize"].Unit)
	require.NotNil(t, props["chunkSize"].Maximum)
	assert.Equal(t, float64(8000), *props["chunkSize"].Maximum)
	assert.Equal(t, 120, props["timeout"].Default)
	for _, visible := range []string{"connection", "model", "collection", "chunkSize", "timeout"} {
		assert.Truef(t, props[visible].AgentVisible, "%s is AgentVisible", visible)
	}
	for _, hidden := range []string{"text", "documentId", "metadata"} {
		assert.Falsef(t, props[hidden].AgentVisible, "%s is not AgentVisible", hidden)
	}
	assert.Contains(t, info.Documentation, "## Configuration")
	in, out := mustKnowledgeAdd(t, nil).Ports()
	assert.Equal(t, info.Inputs, in)
	assert.Equal(t, info.Outputs, out)
}

func TestKnowledgeAddRefusesBadSettings(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{"no model", map[string]any{"model": nil}, "model is required"},
		{"blank model", map[string]any{"model": " "}, "model is required"},
		{"no collection", map[string]any{"collection": nil}, "collection is required"},
		{"bad collection", map[string]any{"collection": "my docs"}, "collection must be"},
		{"blank text", map[string]any{"text": " "}, "text is required"},
		{"documentId not text", map[string]any{"documentId": 3}, "documentId must be text"},
		{"metadata not an object", map[string]any{"metadata": "a=b"}, "metadata must be"},
		{"metadata nested", map[string]any{"metadata": map[string]any{"a": map[string]any{"b": "c"}}}, "metadata"},
		{"metadata empty name", map[string]any{"metadata": map[string]any{" ": "x"}}, "metadata"},
		{"chunkSize small", map[string]any{"chunkSize": 199}, "chunkSize must be 0"},
		{"chunkSize large", map[string]any{"chunkSize": 8001}, "chunkSize must be between 0 and 8000 characters"},
		{"chunkSize negative", map[string]any{"chunkSize": -1}, "chunkSize"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := map[string]any{"connection": "c", "model": "embed", "collection": "docs"}
			for k, v := range tc.cfg {
				if v == nil {
					delete(cfg, k)
				} else {
					cfg[k] = v
				}
			}
			_, err := NewKnowledgeAddAction(cfg)
			assert.ErrorContains(t, err, tc.want)
		})
	}
	mustKnowledgeAdd(t, map[string]any{"chunkSize": 200, "metadata": map[string]any{"source": "{{topic}}", "page": 3, "draft": false}})
}

func TestKnowledgeAddStoresADocument(t *testing.T) {
	f := newFakeEmbedder(t, 9)
	env := f.env(t)
	n := mustKnowledgeAdd(t, map[string]any{
		"text":       "{{payload.title}}: {{payload.body}}",
		"documentId": "note-{{payload.id}}",
		"metadata":   map[string]any{"source": "{{topic}}", "page": 3},
	})
	in := types.NewMessage(map[string]any{"id": 7, "title": "Boiler", "body": "Service it every October."}, types.DataTypeObject)
	in.Topic = "manuals"
	in.SourcePort = "true"
	outs, err := n.Process(env.ctx, in)
	require.NoError(t, err)
	require.Len(t, outs, 1)
	assert.Equal(t, in.Payload, outs[0].Payload, "the message is passed on")
	assert.Equal(t, "output", outs[0].SourcePort)
	assert.Equal(t, map[string]any{
		"connection": fakeLLMConnection, "model": "embed", "collection": "docs",
		"ids": []string{"note-7"}, "inputTokens": int64(9),
	}, outs[0].Meta["knowledge"])

	reqs := f.Requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, []string{"/v1/embeddings"}, f.Paths())
	assert.Equal(t, "embed", reqs[0]["model"])
	assert.Equal(t, []string{"Boiler: Service it every October."}, requestInputs(t, reqs[0]))

	docs, total := env.knowledge.Documents("docs", 0, 10)
	require.Equal(t, 1, total)
	assert.Equal(t, "note-7", docs[0].ID)
	assert.Equal(t, "Boiler: Service it every October.", docs[0].Text)
	assert.Equal(t, "embed", docs[0].Model)
	assert.Equal(t, map[string]any{"source": "manuals", "page": 3}, docs[0].Metadata)
	assert.Equal(t, llm.Usage{InputTokens: 9, Requests: 1}, env.store.UsageToday(fakeLLMConnection))

	// The same ID again replaces the document.
	in.Payload = map[string]any{"id": 7, "title": "Boiler", "body": "Service it every September."}
	_, err = n.Process(env.ctx, in)
	require.NoError(t, err)
	docs, total = env.knowledge.Documents("docs", 0, 10)
	require.Equal(t, 1, total)
	assert.Equal(t, "Boiler: Service it every September.", docs[0].Text)
}

func TestKnowledgeAddNamesNewDocuments(t *testing.T) {
	f := newFakeEmbedder(t, 1)
	env := f.env(t)
	n := mustKnowledgeAdd(t, nil)
	for i := 0; i < 2; i++ {
		outs, err := n.Process(env.ctx, types.NewMessage(fmt.Sprintf("fact %d", i), types.DataTypeString))
		require.NoError(t, err)
		ids := outs[0].Meta["knowledge"].(map[string]any)["ids"].([]string)
		require.Len(t, ids, 1)
		_, err = uuid.Parse(ids[0])
		assert.NoErrorf(t, err, "%s is a UUID", ids[0])
	}
	assert.Len(t, documentIDs(env.knowledge, "docs"), 2, "each message is a new document")
}

func TestKnowledgeAddChunks(t *testing.T) {
	f := newFakeEmbedder(t, 5)
	env := f.env(t)
	n := mustKnowledgeAdd(t, map[string]any{"chunkSize": 200, "documentId": "{{payload.name}}", "text": "{{payload.text}}"})

	var paragraphs []string
	for i := 0; i < 6; i++ {
		paragraphs = append(paragraphs, strings.Repeat(fmt.Sprintf("Paragraph %d says something. ", i), 3))
	}
	long := strings.Join(paragraphs, "\n\n")
	chunks := llm.Chunk(long, 200)
	require.Greater(t, len(chunks), 2)

	// Documents of other IDs stay, whatever they look like.
	for _, id := range []string{"manual-old", "manualx#1", "manual#notes"} {
		require.NoError(t, env.knowledge.Add("docs", llm.Document{ID: id, Text: "other", Model: "embed", Embedding: llm.Embedding{1, 1}}))
	}
	// An earlier whole document of the same ID is replaced by the chunks.
	require.NoError(t, env.knowledge.Add("docs", llm.Document{ID: "manual", Text: "old whole", Model: "embed", Embedding: llm.Embedding{1, 2}}))

	outs, err := n.Process(env.ctx, types.NewMessage(map[string]any{"name": "manual", "text": long}, types.DataTypeObject))
	require.NoError(t, err)
	var want []string
	for i := range chunks {
		want = append(want, fmt.Sprintf("manual#%d", i+1))
	}
	meta := outs[0].Meta["knowledge"].(map[string]any)
	assert.Equal(t, want, meta["ids"])
	assert.Equal(t, int64(5), meta["inputTokens"])
	reqs := f.Requests()
	require.Len(t, reqs, 1, "one request for a few chunks")
	assert.Equal(t, chunks, requestInputs(t, reqs[0]))
	assert.Equal(t, sortedStrings(append(want, "manual#notes", "manual-old", "manualx#1")), documentIDs(env.knowledge, "docs"))

	// A shorter text later: the chunks it no longer has are removed.
	outs, err = n.Process(env.ctx, types.NewMessage(map[string]any{"name": "manual", "text": paragraphs[0]}, types.DataTypeObject))
	require.NoError(t, err)
	assert.Equal(t, []string{"manual#1"}, outs[0].Meta["knowledge"].(map[string]any)["ids"])
	assert.Equal(t, []string{"manual#1", "manual#notes", "manual-old", "manualx#1"}, documentIDs(env.knowledge, "docs"))
	docs, _ := env.knowledge.Documents("docs", 0, 10)
	for _, d := range docs {
		if d.ID == "manual#1" {
			assert.Equal(t, strings.TrimSpace(paragraphs[0]), d.Text)
		}
	}

	// And unchunked again: the chunks go, the whole document comes back.
	whole := mustKnowledgeAdd(t, map[string]any{"documentId": "{{payload.name}}", "text": "{{payload.text}}"})
	_, err = whole.Process(env.ctx, types.NewMessage(map[string]any{"name": "manual", "text": "short"}, types.DataTypeObject))
	require.NoError(t, err)
	assert.Equal(t, []string{"manual", "manual#notes", "manual-old", "manualx#1"}, documentIDs(env.knowledge, "docs"))
}

// Many chunks are embedded in batches, their tokens summed.
func TestKnowledgeAddEmbedsInBatches(t *testing.T) {
	f := newFakeEmbedder(t, 3)
	env := f.env(t)
	n := mustKnowledgeAdd(t, map[string]any{"chunkSize": 200, "documentId": "big"})
	var sentences []string
	for i := 0; len(sentences) < knowledgeEmbedBatch*3; i++ {
		sentences = append(sentences, fmt.Sprintf("Sentence number %04d is here to fill the chunk with plenty of words.", i))
	}
	text := strings.Join(sentences, " ")
	chunks := llm.Chunk(text, 200)
	require.Greater(t, len(chunks), knowledgeEmbedBatch)

	outs, err := n.Process(env.ctx, types.NewMessage(text, types.DataTypeString))
	require.NoError(t, err)
	reqs := f.Requests()
	batches := (len(chunks) + knowledgeEmbedBatch - 1) / knowledgeEmbedBatch
	require.Len(t, reqs, batches)
	var sent []string
	for _, r := range reqs {
		inputs := requestInputs(t, r)
		assert.LessOrEqual(t, len(inputs), knowledgeEmbedBatch)
		sent = append(sent, inputs...)
	}
	assert.Equal(t, chunks, sent)
	meta := outs[0].Meta["knowledge"].(map[string]any)
	assert.Equal(t, int64(3*batches), meta["inputTokens"])
	assert.Len(t, meta["ids"], len(chunks))
	assert.Len(t, documentIDs(env.knowledge, "docs"), len(chunks))
}

// What cannot be stored is refused before any token is spent.
func TestKnowledgeAddRefusesBeforeEmbedding(t *testing.T) {
	f := newFakeEmbedder(t, 1)

	t.Run("an empty text", func(t *testing.T) {
		env := f.env(t)
		_, err := mustKnowledgeAdd(t, map[string]any{"text": "{{payload.missing}}"}).
			Process(env.ctx, types.NewMessage(map[string]any{"other": 1}, types.DataTypeObject))
		assert.ErrorContains(t, err, "text is empty")
	})

	t.Run("a text too long for one document", func(t *testing.T) {
		env := f.env(t)
		_, err := mustKnowledgeAdd(t, nil).
			Process(env.ctx, types.NewMessage(strings.Repeat("a ", llm.MaxDocumentBytes), types.DataTypeString))
		assert.ErrorContains(t, err, "chunkSize")
	})

	t.Run("too many chunks", func(t *testing.T) {
		env := f.env(t)
		_, err := mustKnowledgeAdd(t, map[string]any{"chunkSize": 200}).
			Process(env.ctx, types.NewMessage(strings.Repeat("word ", 50*knowledgeMaxChunks), types.DataTypeString))
		assert.ErrorContains(t, err, "chunks")
	})

	t.Run("an ID that renders empty", func(t *testing.T) {
		env := f.env(t)
		_, err := mustKnowledgeAdd(t, map[string]any{"documentId": "{{payload.id}}"}).
			Process(env.ctx, types.NewMessage(map[string]any{"text": "x"}, types.DataTypeObject))
		assert.ErrorContains(t, err, "documentId")
	})

	t.Run("an ID too long for its chunks", func(t *testing.T) {
		env := f.env(t)
		_, err := mustKnowledgeAdd(t, map[string]any{"documentId": strings.Repeat("i", llm.MaxDocumentIDLength-1), "chunkSize": 200}).
			Process(env.ctx, types.NewMessage("x", types.DataTypeString))
		assert.ErrorContains(t, err, "documentId")
	})

	t.Run("an ID on two lines", func(t *testing.T) {
		env := f.env(t)
		_, err := mustKnowledgeAdd(t, map[string]any{"documentId": "{{payload.id}}"}).
			Process(env.ctx, types.NewMessage(map[string]any{"id": "a\nb"}, types.DataTypeObject))
		assert.ErrorContains(t, err, "documentId")
	})

	t.Run("metadata too large", func(t *testing.T) {
		env := f.env(t)
		_, err := mustKnowledgeAdd(t, map[string]any{"metadata": map[string]any{"body": "{{payload}}"}}).
			Process(env.ctx, types.NewMessage(strings.Repeat("m", llm.MaxMetadataBytes), types.DataTypeString))
		assert.ErrorContains(t, err, "metadata is too large")
	})

	t.Run("a collection of another model", func(t *testing.T) {
		env := f.env(t)
		require.NoError(t, env.knowledge.Add("docs", llm.Document{ID: "a", Text: "x", Model: "other-embed", Embedding: llm.Embedding{1}}))
		_, err := mustKnowledgeAdd(t, nil).Process(env.ctx, types.NewMessage("x", types.DataTypeString))
		require.ErrorIs(t, err, llm.ErrModelMismatch)
		assert.Contains(t, err.Error(), "other-embed")
		assert.False(t, node.IsRetryable(err))
	})

	t.Run("an Anthropic connection", func(t *testing.T) {
		env := f.env(t, func(c *llm.Connection) { c.Provider = llm.ProviderAnthropic; c.APIKey = "sk-ant-test" })
		_, err := mustKnowledgeAdd(t, nil).Process(env.ctx, types.NewMessage("x", types.DataTypeString))
		require.ErrorIs(t, err, llm.ErrUnsupported)
		assert.Contains(t, err.Error(), "embeddings")
	})

	assert.Empty(t, f.Requests())
}

// Embeddings of other dimensions under the same model name are refused by
// the knowledge base, after the provider answered.
func TestKnowledgeAddDimensionMismatch(t *testing.T) {
	f := newFakeEmbedder(t, 1)
	env := f.env(t)
	require.NoError(t, env.knowledge.Add("docs", llm.Document{ID: "a", Text: "x", Model: "embed", Embedding: llm.Embedding{1, 2, 3}}))
	_, err := mustKnowledgeAdd(t, nil).Process(env.ctx, types.NewMessage("x", types.DataTypeString))
	require.ErrorIs(t, err, llm.ErrModelMismatch)
	assert.Equal(t, []string{"a"}, documentIDs(env.knowledge, "docs"))
}

// A document ID may be llm.MaxDocumentIDLength characters, its chunk
// suffix included.
func TestKnowledgeAddIDLength(t *testing.T) {
	f := newFakeEmbedder(t, 1)
	env := f.env(t)
	whole := strings.Repeat("i", llm.MaxDocumentIDLength)
	_, err := mustKnowledgeAdd(t, map[string]any{"documentId": whole}).Process(env.ctx, types.NewMessage("x", types.DataTypeString))
	require.NoError(t, err)
	chunked := strings.Repeat("c", llm.MaxDocumentIDLength-2)
	outs, err := mustKnowledgeAdd(t, map[string]any{"documentId": chunked, "chunkSize": 200}).Process(env.ctx, types.NewMessage("x", types.DataTypeString))
	require.NoError(t, err)
	assert.Equal(t, []string{chunked + "#1"}, outs[0].Meta["knowledge"].(map[string]any)["ids"])
}
