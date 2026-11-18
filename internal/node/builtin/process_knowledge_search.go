package builtin

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
)

// KnowledgeSearchProcess finds the documents of a knowledge collection
// closest in meaning to a query built from the message (docs/design/llm.md
// §3) and passes them on with a context text ready for a prompt.
type KnowledgeSearchProcess struct {
	*node.BaseNode
	llmSettings

	collection  string
	query       string // template
	topK        int
	minScore    float64
	resultField string
}

const (
	knowledgeDefaultTopK = 4
	knowledgeMaxTopK     = 50
	// knowledgeContextSeparator separates the documents' texts in context.
	knowledgeContextSeparator = "\n\n---\n\n"
)

// NewKnowledgeSearchProcess creates a KnowledgeSearchProcess from
// configuration.
func NewKnowledgeSearchProcess(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)
	cfg := base.GetConfig()
	settings, err := parseLLMSettings(base, llmDefaultTimeout)
	if err != nil {
		return nil, err
	}
	p := &KnowledgeSearchProcess{BaseNode: base, llmSettings: settings}
	if p.collection, err = parseKnowledgeCollection(base); err != nil {
		return nil, err
	}
	if p.query, err = templateSetting(cfg, "query", "{{payload}}"); err != nil {
		return nil, err
	}
	if p.topK, err = intSetting(cfg, "topK", knowledgeDefaultTopK, 1, knowledgeMaxTopK, "documents"); err != nil {
		return nil, err
	}
	if p.minScore, err = scoreSetting(cfg, "minScore"); err != nil {
		return nil, err
	}
	if p.resultField, err = parseResultField(base); err != nil {
		return nil, err
	}
	return p, nil
}

// scoreSetting reads a similarity score between 0 and 1 (0 when absent).
func scoreSetting(cfg map[string]any, key string) (float64, error) {
	v := cfg[key]
	if v == nil {
		return 0, nil
	}
	f, ok := numberValue(v)
	if !ok {
		return 0, fmt.Errorf("%s must be a number between 0 and 1", key)
	}
	if math.IsNaN(f) || f < 0 || f > 1 {
		return 0, fmt.Errorf("%s must be between 0 and 1", key)
	}
	return f, nil
}

// Process searches the collection and passes the message on with what it
// found.
func (p *KnowledgeSearchProcess) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	var out *types.Message
	err := p.run(ctx, func(ctx context.Context) error {
		kb := llm.KnowledgeFromContext(ctx)
		info, found := knowledgeCollection(kb, p.collection)
		settings := p.llmSettings
		if settings.model == "" && found {
			// The query must be embedded by the model the collection was
			// built with; the connection's default is usually a chat model.
			settings.model = info.Model
		}
		s, err := settings.open(ctx, "")
		if err != nil {
			return err
		}
		if err := unsupportedOnAnthropic(s.conn, "embeddings", llmEmbeddingProviders); err != nil {
			return err
		}
		query := strings.TrimSpace(renderTemplate(p.query, msg, ctx))
		if query == "" {
			return errors.New("the query is empty for this message")
		}
		var hits []llm.Hit
		var usage llm.Usage
		if found {
			// An empty collection has nothing to compare with: no tokens.
			if hits, usage, err = p.search(ctx, s, kb, info, query); err != nil {
				return err
			}
		}
		out = llmResult(msg, p.resultField, searchResult(query, hits))
		out.SetMeta("knowledge", map[string]any{
			"connection":  s.conn.Name,
			"model":       s.model,
			"collection":  p.collection,
			"inputTokens": usage.InputTokens,
		})
		out.SourcePort = "output"
		return nil
	})
	if err != nil {
		return nil, err
	}
	return []*types.Message{out}, nil
}

// search embeds the query and returns the best documents of the collection
// (info, as looked up for this message).
func (p *KnowledgeSearchProcess) search(ctx context.Context, s llmSession, kb *llm.Knowledge, info llm.CollectionInfo, query string) ([]llm.Hit, llm.Usage, error) {
	if err := collectionModelMismatch(info, s.model); err != nil {
		return nil, llm.Usage{}, err
	}
	embs, usage, err := s.client.Embed(ctx, s.model, []string{query})
	if err != nil {
		return nil, usage, err
	}
	if len(embs) != 1 {
		return nil, usage, fmt.Errorf("the LLM provider answered %d embeddings for one query", len(embs))
	}
	hits, err := kb.Search(p.collection, s.model, embs[0], p.topK, p.minScore)
	if err != nil {
		return nil, usage, err
	}
	return hits, usage, nil
}

// searchResult is the node's result: the query, the hits (best first) and
// their texts joined for a prompt. The values are JSON-shaped (templates
// and expressions read them like any payload), and each result's metadata
// is a copy: the message must not share the stored document's map.
func searchResult(query string, hits []llm.Hit) map[string]any {
	results := make([]any, 0, len(hits))
	texts := make([]string, 0, len(hits))
	for _, h := range hits {
		metadata := make(map[string]any, len(h.Metadata))
		for k, v := range h.Metadata {
			metadata[k] = v
		}
		results = append(results, map[string]any{"id": h.ID, "text": h.Text, "score": h.Score, "metadata": metadata})
		texts = append(texts, h.Text)
	}
	return map[string]any{"query": query, "results": results, "context": strings.Join(texts, knowledgeContextSeparator)}
}

// Ports returns the port definitions.
func (p *KnowledgeSearchProcess) Ports() (inputs []types.Port, outputs []types.Port) {
	return llmInputs, llmOutputs
}

// Validate checks the configuration (the constructor already did).
func (p *KnowledgeSearchProcess) Validate() error { return nil }

func knowledgeSearchConfigSchema() node.ConfigSchema {
	one, maxK := float64(1), float64(knowledgeMaxTopK)
	zero := float64(0)
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"connection": llmSchemaConnection(),
			"model":      llmSchemaModel("Embedding model of the collection (empty: the model the collection was built with)"),
			"collection": llmSchemaCollection("Collection to search, e.g. manuals"),
			"query": {Type: "string", Title: "Query",
				Description: "What to look for, built from the message (supports {{templates}})", Default: "{{payload}}"},
			"topK": {Type: "integer", Title: "Results",
				Description: fmt.Sprintf("Most documents to return, best first, in documents (1 to %d)", knowledgeMaxTopK),
				Default:     knowledgeDefaultTopK, Minimum: &one, Maximum: &maxK, Unit: "documents", AgentVisible: true},
			"minScore": {Type: "number", Title: "Minimum score",
				Description: "Leave out documents less similar than this, from 0 (keep all) to 1 (identical)",
				Default:     zero, Minimum: &zero, Maximum: &one, AgentVisible: true},
			"resultField": llmSchemaResultField(),
			"timeout":     llmSchemaTimeout(llmDefaultTimeout),
		},
		Required: []string{"connection", "collection", "query"},
	}
}

// GetConfigSchema returns the configuration schema for the UI.
func (p *KnowledgeSearchProcess) GetConfigSchema() node.ConfigSchema {
	return knowledgeSearchConfigSchema()
}

// KnowledgeSearchProcessInfo returns the node type info for registration.
func KnowledgeSearchProcessInfo() node.NodeTypeInfo {
	schema := knowledgeSearchConfigSchema()
	return node.NodeTypeInfo{
		Type:        "process-knowledge-search",
		Name:        "Search Knowledge Base",
		Description: "Find the stored documents closest in meaning to a query",
		Documentation: `## Search Knowledge Base

Finds the documents of a knowledge collection (filled by **Add to Knowledge
Base**) closest in meaning to a query built from the message, and passes
them on with a ` + "`context`" + ` text ready for an **LLM Prompt**: answers grounded
in your own documents. The query is embedded by the collection's embedding
model through an OpenAI-compatible **LLM connection**; the search itself
runs in PiWorker.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| connection | string | - | LLM connection (required; OpenAI-compatible) |
| model | string | collection's | Embedding model (empty: the one the collection was built with) |
| collection | string | - | Collection to search (required) |
| query | string | {{payload}} | What to look for (templates) |
| topK | integer | 4 | Most documents to return, in documents (1-50) |
| minScore | number | 0 | Leave out documents less similar than this (0-1) |
| resultField | string | - | Payload field for the result (empty: replace the payload) |
| timeout | integer | 120 | Longest time for the whole call, in seconds (1-600) |

## Output

` + "```" + `
{
  "query": "when is the boiler serviced?",
  "results": [
    {"id": "manual#3", "text": "...", "score": 0.82, "metadata": {"source": "manual"}}
  ],
  "context": "text of the first result\n\n---\n\ntext of the second"
}
` + "```" + `

as the payload or in ` + "`resultField`" + `. **score** is the cosine similarity
(1: the same meaning). An empty or missing collection gives no results and
spends no tokens. **meta.knowledge** holds the connection, the model, the
collection and the ` + "`inputTokens`" + ` spent.

## Example

Answer questions from your notes: search with ` + "`resultField: kb`" + `, then an
**LLM Prompt** with

` + "```" + `
system: Answer only from these notes:
  {{payload.kb.context}}
prompt: {{payload.question}}
` + "```" + `

## Errors

An empty query, a model other than the collection's, a spent daily limit
and a timeout fail the message. Rate limits, overloaded providers and
network errors are transient: the flow's retries apply to them.

## Privacy

The query is sent to the connection's provider to be embedded; the
documents never are. With a local model (Ollama) the query stays on your
network.
`,
		Category: types.NodeCategoryProcessing,
		Risk:     node.RiskExternal,
		Inputs:   llmInputs,
		Outputs:  llmOutputs,
		Config:   &schema,
		Icon:     "book-open",
	}
}

func init() {
	info := KnowledgeSearchProcessInfo()
	if err := node.Register(info, NewKnowledgeSearchProcess); err != nil {
		panic("failed to register knowledge search process: " + err.Error())
	}
}
