package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Knowledge base limits.
const (
	// MaxDocumentIDLength bounds a document ID, in characters.
	MaxDocumentIDLength = 200
	// MaxDocumentBytes bounds a document's text.
	MaxDocumentBytes = 32 << 10
	// MaxDocuments bounds the documents of one collection.
	MaxDocuments = 20000
	// MaxDimensions bounds an embedding's dimensions.
	MaxDimensions = 8192
	// MaxMetadataBytes bounds a document's metadata once encoded.
	MaxMetadataBytes = 8 << 10
)

// ErrModelMismatch: an embedding does not match its collection (another
// model or other dimensions): embeddings of different models cannot be
// compared.
var ErrModelMismatch = errors.New("the embedding model does not match the collection")

// KnowledgePersister persists the knowledge base and provides the initial
// load. The SQLite store implements it.
type KnowledgePersister interface {
	LoadLLMDocuments() (map[string][]Document, error)
	SaveLLMDocument(collection string, d Document) error
	DeleteLLMDocument(collection, id string) error
	DeleteLLMCollection(collection string) error
}

// Knowledge is a thread-safe knowledge base: collections of documents with
// their embeddings, searched by cosine similarity (brute force: a Pi holds
// thousands of documents, not millions).
type Knowledge struct {
	mu      sync.RWMutex
	cols    map[string]*collection
	persist KnowledgePersister

	maxDocuments int // MaxDocuments (tests lower it)
}

type collection struct {
	docs    map[string]*Document
	model   string
	dims    int
	updated time.Time
}

// NewKnowledge creates an empty in-memory knowledge base.
func NewKnowledge() *Knowledge {
	return &Knowledge{cols: map[string]*collection{}, maxDocuments: MaxDocuments}
}

// Attach loads the persisted documents and enables write-through.
func (k *Knowledge) Attach(p KnowledgePersister) error {
	loaded, err := p.LoadLLMDocuments()
	if err != nil {
		return fmt.Errorf("failed to load the knowledge base: %w", err)
	}
	cols := map[string]*collection{}
	for name, docs := range loaded {
		for _, d := range docs {
			unit, err := normalize(d.Embedding)
			if err != nil {
				continue // a stored zero vector can never match
			}
			c := cols[name]
			if c == nil {
				c = &collection{docs: map[string]*Document{}, model: d.Model, dims: len(unit)}
				cols[name] = c
			}
			if d.Model != c.model || len(unit) != c.dims {
				continue
			}
			doc := d
			doc.Embedding = unit
			c.docs[d.ID] = &doc
			if d.CreatedAt.After(c.updated) {
				c.updated = d.CreatedAt
			}
		}
	}
	k.mu.Lock()
	k.cols = cols
	k.persist = p
	k.mu.Unlock()
	return nil
}

// validDocument checks d against the limits (not against its collection).
func validDocument(collectionName string, d Document) error {
	if !ValidName(collectionName) {
		return fmt.Errorf("invalid collection name (1 to %d letters, digits, '_' or '-')", MaxNameLength)
	}
	if d.ID == "" || !utf8.ValidString(d.ID) || utf8.RuneCountInString(d.ID) > MaxDocumentIDLength {
		return fmt.Errorf("invalid document ID (1 to %d characters)", MaxDocumentIDLength)
	}
	if len(d.Text) > MaxDocumentBytes {
		return fmt.Errorf("the document's text is too long (at most %d KiB)", MaxDocumentBytes>>10)
	}
	if d.Model == "" {
		return errors.New("the document has no embedding model")
	}
	if len(d.Embedding) == 0 {
		return errors.New("the document has no embedding")
	}
	if len(d.Embedding) > MaxDimensions {
		return fmt.Errorf("the embedding has too many dimensions (at most %d)", MaxDimensions)
	}
	if len(d.Metadata) > 0 {
		raw, err := json.Marshal(d.Metadata)
		if err != nil {
			return fmt.Errorf("the document's metadata cannot be encoded: %w", err)
		}
		if len(raw) > MaxMetadataBytes {
			return fmt.Errorf("the document's metadata is too large (at most %d KiB)", MaxMetadataBytes>>10)
		}
	}
	return nil
}

// normalize returns a unit-length copy of e (an error for a zero vector or
// a non-finite value).
func normalize(e Embedding) (Embedding, error) {
	var sum float64
	for _, v := range e {
		f := float64(v)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, errors.New("the embedding has a value that is not a number")
		}
		sum += f * f
	}
	if sum == 0 {
		return nil, errors.New("the embedding is a zero vector")
	}
	norm := math.Sqrt(sum)
	out := make(Embedding, len(e))
	for i, v := range e {
		out[i] = float32(float64(v) / norm)
	}
	return out, nil
}

// Add stores d in collection, replacing the document with the same ID,
// writing through first. The embedding is normalized to unit length; every
// document of a collection has the same model and dimensions, else
// ErrModelMismatch. CreatedAt defaults to now.
func (k *Knowledge) Add(collectionName string, d Document) error {
	if err := validDocument(collectionName, d); err != nil {
		return err
	}
	unit, err := normalize(d.Embedding)
	if err != nil {
		return err
	}
	d.Embedding = unit
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now()
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	c := k.cols[collectionName]
	if c != nil {
		_, replacing := c.docs[d.ID]
		onlyThis := replacing && len(c.docs) == 1
		if !onlyThis && (d.Model != c.model || len(unit) != c.dims) {
			return fmt.Errorf("%w: collection %q holds %d-dimension embeddings of %q, not %d-dimension embeddings of %q",
				ErrModelMismatch, collectionName, c.dims, c.model, len(unit), d.Model)
		}
		if !replacing && len(c.docs) >= k.maxDocuments {
			return fmt.Errorf("collection %q is full (at most %d documents)", collectionName, k.maxDocuments)
		}
	}
	if k.persist != nil {
		if err := k.persist.SaveLLMDocument(collectionName, d); err != nil {
			return err
		}
	}
	if c == nil {
		c = &collection{docs: map[string]*Document{}}
		k.cols[collectionName] = c
	}
	c.model, c.dims = d.Model, len(unit)
	c.docs[d.ID] = &d
	if d.CreatedAt.After(c.updated) {
		c.updated = d.CreatedAt
	} else {
		c.updated = time.Now()
	}
	return nil
}

// Search returns the k documents of collection most similar to query
// (cosine similarity, best first) whose score is at least minScore. A
// collection that does not exist has no hits; one of another model or other
// dimensions is ErrModelMismatch. Hits carry no embedding.
func (k *Knowledge) Search(collectionName, model string, query Embedding, topK int, minScore float64) ([]Hit, error) {
	if topK <= 0 {
		return nil, nil
	}
	q, err := normalize(query)
	if err != nil {
		return nil, err
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	c := k.cols[collectionName]
	if c == nil || len(c.docs) == 0 {
		return nil, nil
	}
	if model != c.model || len(q) != c.dims {
		return nil, fmt.Errorf("%w: collection %q holds %d-dimension embeddings of %q, the query is a %d-dimension embedding of %q",
			ErrModelMismatch, collectionName, c.dims, c.model, len(q), model)
	}
	hits := make([]Hit, 0, min(topK*4, len(c.docs)))
	for _, d := range c.docs {
		var dot float64
		for i, v := range d.Embedding {
			dot += float64(v) * float64(q[i])
		}
		if dot < minScore {
			continue
		}
		h := Hit{Document: *d, Score: dot}
		h.Embedding = nil
		hits = append(hits, h)
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].ID < hits[j].ID
	})
	if len(hits) > topK {
		hits = hits[:topK]
	}
	return hits, nil
}

// Delete removes one document after its persistent deletion succeeds.
func (k *Knowledge) Delete(collectionName, id string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.persist != nil {
		if err := k.persist.DeleteLLMDocument(collectionName, id); err != nil {
			return err
		}
	}
	if c := k.cols[collectionName]; c != nil {
		delete(c.docs, id)
		if len(c.docs) == 0 {
			delete(k.cols, collectionName)
		} else {
			c.updated = time.Now()
		}
	}
	return nil
}

// DeleteCollection removes a collection after its persistent deletion
// succeeds.
func (k *Knowledge) DeleteCollection(collectionName string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.persist != nil {
		if err := k.persist.DeleteLLMCollection(collectionName); err != nil {
			return err
		}
	}
	delete(k.cols, collectionName)
	return nil
}

// Collections summarizes the collections, sorted by name.
func (k *Knowledge) Collections() []CollectionInfo {
	k.mu.RLock()
	out := make([]CollectionInfo, 0, len(k.cols))
	for name, c := range k.cols {
		out = append(out, CollectionInfo{Name: name, Documents: len(c.docs), Model: c.model, Dimensions: c.dims, UpdatedAt: c.updated})
	}
	k.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Documents returns a page of a collection's documents (oldest first, then
// by ID) without their embeddings, and the collection's document count.
func (k *Knowledge) Documents(collectionName string, offset, limit int) ([]Document, int) {
	k.mu.RLock()
	c := k.cols[collectionName]
	if c == nil {
		k.mu.RUnlock()
		return nil, 0
	}
	all := make([]Document, 0, len(c.docs))
	for _, d := range c.docs {
		doc := *d
		doc.Embedding = nil
		all = append(all, doc)
	}
	k.mu.RUnlock()
	sort.Slice(all, func(i, j int) bool {
		if !all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].CreatedAt.Before(all[j].CreatedAt)
		}
		return all[i].ID < all[j].ID
	})
	total := len(all)
	if offset < 0 {
		offset = 0
	}
	if offset >= total || limit <= 0 {
		return []Document{}, total
	}
	end := min(offset+limit, total)
	return all[offset:end], total
}

// DefaultKnowledge is the process-wide knowledge base the nodes use.
var DefaultKnowledge = NewKnowledge()

type knowledgeKey struct{}

// WithKnowledge returns a context whose nodes use k instead of
// DefaultKnowledge; a nil k means an empty one (isolation fails closed).
func WithKnowledge(ctx context.Context, k *Knowledge) context.Context {
	if k == nil {
		k = NewKnowledge()
	}
	return context.WithValue(ctx, knowledgeKey{}, k)
}

// KnowledgeFromContext returns the knowledge base carried by ctx, or
// DefaultKnowledge.
func KnowledgeFromContext(ctx context.Context) *Knowledge {
	if ctx != nil {
		if k, ok := ctx.Value(knowledgeKey{}).(*Knowledge); ok && k != nil {
			return k
		}
	}
	return DefaultKnowledge
}

// Chunking separators, from the coarsest: paragraphs, sentences, words.
var (
	paragraphEnd = regexp.MustCompile(`\n[ \t]*\n\s*`)
	sentenceEnd  = regexp.MustCompile(`[.!?。！？]+["'”’)\]]*\s+`)
	wordEnd      = regexp.MustCompile(`\s+`)
)

// Chunk splits text into chunks of at most size runes for embedding: on
// paragraph boundaries, then sentence, then word boundaries (a word longer
// than a chunk is cut), each chunk repeating about the last 10 % of the
// previous one so a passage cut in two is still found. A text of at most
// size runes (or size ≤ 0) is one chunk; a blank text none.
func Chunk(text string, size int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if size <= 0 || utf8.RuneCountInString(text) <= size {
		return []string{text}
	}
	overlap := size / 10
	maxPiece := size - overlap
	pieces := splitPieces(text, maxPiece, []*regexp.Regexp{paragraphEnd, sentenceEnd, wordEnd})

	var chunks []string
	var cur strings.Builder
	curLen := 0
	fresh := false // cur holds a piece that is not only the previous overlap
	for _, p := range pieces {
		pl := utf8.RuneCountInString(p)
		if fresh && curLen+pl > size {
			chunk := cur.String()
			chunks = append(chunks, strings.TrimSpace(chunk))
			tail := overlapTail(chunk, overlap)
			cur.Reset()
			cur.WriteString(tail)
			curLen = utf8.RuneCountInString(tail)
		}
		cur.WriteString(p)
		curLen += pl
		fresh = true
	}
	if last := strings.TrimSpace(cur.String()); fresh && last != "" {
		chunks = append(chunks, last)
	}
	return chunks
}

// splitPieces cuts text into consecutive pieces (their concatenation is
// text) of at most max runes, with the coarsest separator that does it.
func splitPieces(text string, max int, seps []*regexp.Regexp) []string {
	if utf8.RuneCountInString(text) <= max {
		return []string{text}
	}
	if len(seps) == 0 {
		var out []string
		runes := []rune(text)
		for len(runes) > 0 {
			n := min(max, len(runes))
			out = append(out, string(runes[:n]))
			runes = runes[n:]
		}
		return out
	}
	var out []string
	for _, part := range splitAfter(text, seps[0]) {
		out = append(out, splitPieces(part, max, seps[1:])...)
	}
	return out
}

// splitAfter splits s after each match of re (the separator stays with the
// part before it).
func splitAfter(s string, re *regexp.Regexp) []string {
	var out []string
	start := 0
	for _, loc := range re.FindAllStringIndex(s, -1) {
		if loc[1] > start {
			out = append(out, s[start:loc[1]])
			start = loc[1]
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// overlapTail returns the end of chunk to repeat at the start of the next
// one: about its last n runes, starting at a word.
func overlapTail(chunk string, n int) string {
	if n <= 0 {
		return ""
	}
	trimmed := strings.TrimRight(chunk, " \t\r\n")
	runes := []rune(trimmed)
	if len(runes) <= n {
		return ""
	}
	tail := string(runes[len(runes)-n:])
	i := strings.IndexFunc(tail, func(r rune) bool { return r == ' ' || r == '\n' || r == '\t' })
	if i < 0 {
		return ""
	}
	tail = strings.TrimLeft(tail[i:], " \t\r\n")
	if tail == "" {
		return ""
	}
	return tail + " "
}
