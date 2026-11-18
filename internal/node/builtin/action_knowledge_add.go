package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
)

// KnowledgeAddAction stores text from the message in a collection of the
// local knowledge base (docs/design/llm.md §3): it embeds the text (or its
// chunks) with the connection's embedding model and keeps the embeddings
// in PiWorker's database, where Search Knowledge Base finds them.
type KnowledgeAddAction struct {
	*node.BaseNode
	llmSettings

	collection string
	text       string         // template
	documentID string         // template ("" = a new UUID per message)
	metadata   map[string]any // text values are templates
	chunkSize  int            // runes; 0 = one document
}

// Bounds of chunkSize (0 is one document) and of what one message adds.
const (
	knowledgeMinChunk = 200
	knowledgeMaxChunk = 8000
	// knowledgeMaxChunks bounds the chunks of one message (a collection
	// holds llm.MaxDocuments).
	knowledgeMaxChunks = 1000
	// knowledgeEmbedBatch is how many chunks one embeddings request sends:
	// few enough to stay under providers' per-request token limits with
	// long chunks.
	knowledgeEmbedBatch = 32
)

// NewKnowledgeAddAction creates a KnowledgeAddAction from configuration.
func NewKnowledgeAddAction(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)
	cfg := base.GetConfig()
	settings, err := parseLLMSettings(base, llmDefaultTimeout)
	if err != nil {
		return nil, err
	}
	if settings.model == "" {
		// The connection's default model is usually a chat model, and a
		// collection must keep one embedding model: name it here.
		return nil, errors.New("model is required: the embedding model, e.g. text-embedding-3-small (OpenAI) or nomic-embed-text (Ollama)")
	}
	a := &KnowledgeAddAction{BaseNode: base, llmSettings: settings}
	if a.collection, err = parseKnowledgeCollection(base); err != nil {
		return nil, err
	}
	if a.text, err = templateSetting(cfg, "text", "{{payload}}"); err != nil {
		return nil, err
	}
	if a.documentID, err = textSetting(cfg, "documentId"); err != nil {
		return nil, err
	}
	a.documentID = strings.TrimSpace(a.documentID)
	if !strings.Contains(a.documentID, "{{") && llm.LooksLikeChunk(a.documentID) {
		return nil, errChunkLikeID("documentId")
	}
	if a.metadata, err = parseKnowledgeMetadata(cfg); err != nil {
		return nil, err
	}
	if a.chunkSize, err = intSetting(cfg, "chunkSize", 0, 0, knowledgeMaxChunk, "characters"); err != nil {
		return nil, err
	}
	if a.chunkSize > 0 && a.chunkSize < knowledgeMinChunk {
		return nil, fmt.Errorf("chunkSize must be 0 (one document) or between %d and %d characters", knowledgeMinChunk, knowledgeMaxChunk)
	}
	return a, nil
}

// parseKnowledgeMetadata reads metadata: an object whose text values are
// templates and whose numbers and booleans are kept as they are.
func parseKnowledgeMetadata(cfg map[string]any) (map[string]any, error) {
	raw, present := cfg["metadata"]
	if !present || raw == nil {
		return nil, nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("metadata must be an object of names and values (templates)")
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		name := strings.TrimSpace(k)
		if name == "" {
			return nil, errors.New("metadata: every value needs a name")
		}
		switch v.(type) {
		case string, bool, float64, int, int64:
		default:
			return nil, fmt.Errorf("metadata: %s must be text (a template), a number or a boolean", name)
		}
		out[name] = v
	}
	return out, nil
}

// Process stores the message's text in the collection and passes the
// message on.
func (a *KnowledgeAddAction) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	var out *types.Message
	err := a.run(ctx, func(ctx context.Context) error {
		s, err := a.open(ctx, "")
		if err != nil {
			return err
		}
		if err := unsupportedOnAnthropic(s.conn, "embeddings", llmEmbeddingProviders); err != nil {
			return err
		}
		id, docs, err := a.documents(ctx, msg, s.model)
		if err != nil {
			return err
		}
		kb := llm.KnowledgeFromContext(ctx)
		if err := checkCollectionModel(kb, a.collection, s.model); err != nil {
			return err
		}
		usage, err := embedDocuments(ctx, s.client, s.model, docs)
		if err != nil {
			return err
		}
		// The new version and the removal of what it no longer has (the
		// whole document when it is now chunked, its chunks when it is
		// not, chunks past the new last one) are one atomic replace.
		if err := kb.Replace(a.collection, id, docs); err != nil {
			return fmt.Errorf("cannot store the document in collection %q: %w", a.collection, err)
		}
		ids := make([]string, len(docs))
		for i, d := range docs {
			ids[i] = d.ID
		}
		out = msg.Clone()
		out.SetMeta("knowledge", map[string]any{
			"connection":  s.conn.Name,
			"model":       s.model,
			"collection":  a.collection,
			"ids":         ids,
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

// documents returns the document ID of msg and its documents (without
// their embeddings): the rendered text as one document of that ID, or its
// chunks with IDs "<id>#<n>" (n from 1). Everything that cannot be stored
// is refused here, before any token is spent. Errors never quote message
// data.
func (a *KnowledgeAddAction) documents(ctx context.Context, msg *types.Message, model string) (string, []llm.Document, error) {
	text := strings.TrimSpace(renderTemplate(a.text, msg, ctx))
	if text == "" {
		return "", nil, errors.New("the text is empty for this message")
	}
	var texts []string
	if a.chunkSize == 0 {
		if len(text) > llm.MaxDocumentBytes {
			return "", nil, fmt.Errorf("the text is too long for one document (at most %d KiB): set chunkSize to split it", llm.MaxDocumentBytes>>10)
		}
		texts = []string{text}
	} else {
		// Every chunk holds at most chunkSize characters: a text this long
		// cannot fit (counted before the text is split).
		tooMany := fmt.Errorf("the text makes more than %d chunks: raise chunkSize or split the text", knowledgeMaxChunks)
		if utf8.RuneCountInString(text) > knowledgeMaxChunks*a.chunkSize {
			return "", nil, tooMany
		}
		if texts = llm.Chunk(text, a.chunkSize); len(texts) > knowledgeMaxChunks {
			return "", nil, tooMany
		}
	}

	id, err := a.renderID(ctx, msg)
	if err != nil {
		return "", nil, err
	}
	metadata := a.renderMetadata(ctx, msg)
	if raw, err := json.Marshal(metadata); err != nil || len(raw) > llm.MaxMetadataBytes {
		return "", nil, fmt.Errorf("the metadata is too large for this message (at most %d KiB once encoded)", llm.MaxMetadataBytes>>10)
	}
	docs := make([]llm.Document, len(texts))
	for i, t := range texts {
		docID := id
		if a.chunkSize > 0 {
			docID = llm.ChunkID(id, i+1)
		}
		if utf8.RuneCountInString(docID) > llm.MaxDocumentIDLength {
			return "", nil, fmt.Errorf("documentId is too long for this message (at most %d characters, with the chunk number #n when chunkSize is set)", llm.MaxDocumentIDLength)
		}
		docs[i] = llm.Document{ID: docID, Text: t, Metadata: metadata, Model: model}
	}
	return id, docs, nil
}

// renderID renders documentId for msg, or makes a new UUID without one.
func (a *KnowledgeAddAction) renderID(ctx context.Context, msg *types.Message) (string, error) {
	if a.documentID == "" {
		return uuid.NewString(), nil
	}
	id := strings.TrimSpace(renderTemplate(a.documentID, msg, ctx))
	if id == "" {
		return "", errors.New("documentId rendered to an empty ID for this message")
	}
	if !utf8.ValidString(id) || strings.ContainsAny(id, "\x00\r\n") {
		return "", errors.New("documentId rendered to an invalid ID for this message (one line of text)")
	}
	if llm.LooksLikeChunk(id) {
		return "", errChunkLikeID("documentId rendered to an ID that")
	}
	return id, nil
}

// errChunkLikeID refuses a document ID that ends in '#' and a number:
// chunk IDs are "<documentId>#<n>", so such a document would be taken for
// a chunk of another one and deleted when that one is replaced. what says
// which ID ("documentId", or the rendered ID of a message).
func errChunkLikeID(what string) error {
	return fmt.Errorf("%s ends in '#' and a number, the form of chunk IDs (<documentId>#<n>): end it otherwise, e.g. {{payload.ref}}-doc", what)
}

// renderMetadata renders the metadata's templates for msg.
func (a *KnowledgeAddAction) renderMetadata(ctx context.Context, msg *types.Message) map[string]any {
	if len(a.metadata) == 0 {
		return nil
	}
	out := make(map[string]any, len(a.metadata))
	for k, v := range a.metadata {
		if s, ok := v.(string); ok {
			v = renderTemplate(s, msg, ctx)
		}
		out[k] = v
	}
	return out
}

// embedDocuments embeds the documents' texts in batches of
// knowledgeEmbedBatch, setting their embeddings, and returns the usage of
// all the requests. Nothing is stored until every batch is embedded.
func embedDocuments(ctx context.Context, client llm.Client, model string, docs []llm.Document) (llm.Usage, error) {
	var total llm.Usage
	for start := 0; start < len(docs); start += knowledgeEmbedBatch {
		end := min(start+knowledgeEmbedBatch, len(docs))
		inputs := make([]string, 0, end-start)
		for _, d := range docs[start:end] {
			inputs = append(inputs, d.Text)
		}
		embs, usage, err := client.Embed(ctx, model, inputs)
		total = total.Add(usage)
		if err != nil {
			return total, err
		}
		if len(embs) != len(inputs) {
			return total, fmt.Errorf("the LLM provider answered %d embeddings for %d texts", len(embs), len(inputs))
		}
		for i, e := range embs {
			docs[start+i].Embedding = e
		}
	}
	return total, nil
}

// Ports returns the port definitions.
func (a *KnowledgeAddAction) Ports() (inputs []types.Port, outputs []types.Port) {
	return llmInputs, llmOutputs
}

// Validate checks the configuration (the constructor already did).
func (a *KnowledgeAddAction) Validate() error { return nil }

func knowledgeAddConfigSchema() node.ConfigSchema {
	zero, max := float64(0), float64(knowledgeMaxChunk)
	collection := llmSchemaCollection("Collection to store the text in, e.g. manuals (created on the first document)")
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"connection": llmSchemaConnection(),
			"model":      llmSchemaModel("Embedding model, e.g. text-embedding-3-small (OpenAI) or nomic-embed-text (Ollama); every document of a collection uses the same one"),
			"collection": collection,
			"text": {Type: "string", Title: "Text",
				Description: "Text to store, built from the message (supports {{templates}})", Default: "{{payload}}"},
			"documentId": {Type: "string", Title: "Document ID",
				Description: "ID of the document, e.g. {{payload.id}}: adding the same ID again replaces it; it may not end in '#' and a number, the form of chunk IDs (supports {{templates}}; empty: a new ID per message)"},
			"metadata": {Type: "object", Title: "Metadata",
				Description: "Names and values stored with the document and returned by searches, e.g. source = {{topic}} (values support {{templates}})"},
			"chunkSize": {Type: "integer", Title: "Chunk size",
				Description: fmt.Sprintf("Split long texts into chunks of at most this many characters, each one a document (0: one document; else %d to %d characters)", knowledgeMinChunk, knowledgeMaxChunk),
				Default:     0, Minimum: &zero, Maximum: &max, Unit: "characters", AgentVisible: true},
			"timeout": llmSchemaTimeout(llmDefaultTimeout),
		},
		Required: []string{"connection", "model", "collection"},
	}
}

// GetConfigSchema returns the configuration schema for the UI.
func (a *KnowledgeAddAction) GetConfigSchema() node.ConfigSchema { return knowledgeAddConfigSchema() }

// KnowledgeAddActionInfo returns the node type info for registration.
func KnowledgeAddActionInfo() node.NodeTypeInfo {
	schema := knowledgeAddConfigSchema()
	return node.NodeTypeInfo{
		Type:        "action-knowledge-add",
		Name:        "Add to Knowledge Base",
		Description: "Store text in the local knowledge base for later searches",
		Documentation: `## Add to Knowledge Base

Stores text from the message in a **collection** of PiWorker's own knowledge
base, so **Search Knowledge Base** can find it by meaning (not only by
words) and an **LLM Prompt** can answer from it. The text is turned into an
embedding by an embedding model of an OpenAI-compatible **LLM connection**
(OpenAI, or a local model in Ollama); the embeddings stay in PiWorker's
database. The message is passed on unchanged.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| connection | string | - | LLM connection (required; OpenAI-compatible) |
| model | string | - | Embedding model (required) |
| collection | string | - | Collection to store the text in (required) |
| text | string | {{payload}} | Text to store (templates) |
| documentId | string | new ID | Document ID (templates): the same ID replaces the document; not ending in ` + "`#<number>`" + ` |
| metadata | object | - | Names and values stored with the document (values: templates) |
| chunkSize | integer | 0 | Split long texts into chunks of at most this many characters (0: one document; 200-8000) |
| timeout | integer | 120 | Longest time for the whole call, in seconds (1-600) |

Every document of a collection must use the same embedding model: searches
compare embeddings of one model. To change it, delete the collection under
**AI memory** and add the documents again.

## Chunks

A document holds at most 32 KiB of text. Set **chunkSize** (e.g. 1000) to
split long texts on paragraph, sentence and word boundaries; each chunk is a
document with the ID ` + "`<documentId>#<n>`" + `, repeating the end of the
previous chunk so a passage cut in two is still found. Adding the same
**documentId** again replaces all its chunks, at once. A **documentId**
may therefore not end in ` + "`#`" + ` and a number (` + "`INV#1042`" + `): it
would be taken for a chunk of another document. Other IDs with a ` + "`#`" + `
(` + "`INV#A1`" + `, a URL with a fragment) are fine.

## Output

The message, unchanged, with **meta.knowledge**: ` + "`connection`" + `, ` + "`model`" + `,
` + "`collection`" + `, the stored ` + "`ids`" + ` and the ` + "`inputTokens`" + ` spent.

## Example

Keep the notes posted to a webhook, one document per note:

` + "```" + `
collection: notes
text: {{payload.title}}: {{payload.body}}
documentId: note-{{payload.id}}
metadata: source = {{payload.author}}
` + "```" + `

## Errors

An empty text, a text too long for one document without **chunkSize**, a
document ID ending in ` + "`#`" + ` and a number, a collection of another embedding
model, a spent daily limit and a timeout
fail the message. Rate limits, overloaded providers and network errors are
transient: the flow's retries apply to them.

## Privacy

The text is sent to the connection's provider to be embedded; with a local
model (Ollama) it stays on your network.
`,
		Category: types.NodeCategoryOutput,
		Risk:     node.RiskExternal,
		Inputs:   llmInputs,
		Outputs:  llmOutputs,
		Config:   &schema,
		Icon:     "book-plus",
	}
}

func init() {
	info := KnowledgeAddActionInfo()
	if err := node.Register(info, NewKnowledgeAddAction); err != nil {
		panic("failed to register knowledge add action: " + err.Error())
	}
}
