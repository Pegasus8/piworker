package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
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
// load. The SQLite store implements it. The documents LoadLLMDocuments
// returns belong to the knowledge base from then on (Attach normalizes
// their embeddings in place). ReplaceLLMDocuments saves and removes
// documents of one collection in one transaction: all of it or nothing.
type KnowledgePersister interface {
	LoadLLMDocuments() (map[string][]Document, error)
	SaveLLMDocument(collection string, d Document) error
	DeleteLLMDocument(collection, id string) error
	DeleteLLMCollection(collection string) error
	ReplaceLLMDocuments(collection string, save []Document, remove []string) error
}

// ChunkID is the ID of chunk n (from 1) of the document base:
// "<base>#<n>".
func ChunkID(base string, n int) string { return base + "#" + strconv.Itoa(n) }

// IsChunkOf reports whether id is a chunk ID of base ("<base>#<n>").
func IsChunkOf(id, base string) bool { return hasChunkPrefix(id, base+"#") }

// hasChunkPrefix is IsChunkOf with base+"#" already built (prefix), for
// the loops that test many IDs against one base.
func hasChunkPrefix(id, prefix string) bool {
	n, ok := strings.CutPrefix(id, prefix)
	return ok && allDigits(n)
}

// LooksLikeChunk reports whether id ends in '#' and a number, as a chunk
// ID does. A document stored under such an ID would be taken for a chunk
// of another document, and removed when that one is replaced: the
// knowledge-add node refuses it.
func LooksLikeChunk(id string) bool {
	i := strings.LastIndexByte(id, '#')
	return i >= 0 && allDigits(id[i+1:])
}

// allDigits reports whether s is one or more ASCII digits.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
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
			// The loaded embedding is the knowledge base's: normalized in
			// place, not copied (one allocation per document at start).
			if err := normalizeInPlace(d.Embedding); err != nil {
				continue // a stored zero vector can never match
			}
			unit := d.Embedding
			c := cols[name]
			if c == nil {
				c = &collection{docs: map[string]*Document{}, model: d.Model, dims: len(unit)}
				cols[name] = c
			}
			if d.Model != c.model || len(unit) != c.dims {
				continue
			}
			doc := d
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
// a non-finite value). The caller's e is left as it is.
func normalize(e Embedding) (Embedding, error) {
	norm, err := euclideanNorm(e)
	if err != nil {
		return nil, err
	}
	out := make(Embedding, len(e))
	for i, v := range e {
		out[i] = float32(float64(v) / norm)
	}
	return out, nil
}

// normalizeInPlace scales e to unit length, as normalize does, in e itself
// (which the caller owns); on an error e is unchanged.
func normalizeInPlace(e Embedding) error {
	norm, err := euclideanNorm(e)
	if err != nil {
		return err
	}
	for i, v := range e {
		e[i] = float32(float64(v) / norm)
	}
	return nil
}

// euclideanNorm is e's length (an error for a zero vector or a non-finite
// value).
func euclideanNorm(e Embedding) (float64, error) {
	var sum float64
	for _, v := range e {
		f := float64(v)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, errors.New("the embedding has a value that is not a number")
		}
		sum += f * f
	}
	if sum == 0 {
		return 0, errors.New("the embedding is a zero vector")
	}
	return math.Sqrt(sum), nil
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

// Replace stores docs in collection as the whole of the document base:
// each of them is base itself or one of its chunks (ChunkID), and what
// base held that docs do not (the whole document when it is now chunked,
// its chunks when it is not, chunks past the new last one) is removed. It
// is atomic: every document is checked first (limits, one model and
// dimensions, those of the collection's other documents, room), then the
// documents are saved and the stale ones removed in one transaction of
// the persister and under one lock, so concurrent replaces of base leave
// one whole version. A collection holding only base takes another model.
// Embeddings are normalized to unit length; CreatedAt defaults to now.
func (k *Knowledge) Replace(collectionName, base string, docs []Document) error {
	if base == "" {
		return fmt.Errorf("invalid document ID (1 to %d characters)", MaxDocumentIDLength)
	}
	if !ValidName(collectionName) {
		return fmt.Errorf("invalid collection name (1 to %d letters, digits, '_' or '-')", MaxNameLength)
	}
	now := time.Now()
	chunkPrefix := base + "#"
	prepared := make([]Document, 0, len(docs))
	keep := make(map[string]bool, len(docs))
	for _, d := range docs {
		if d.ID != base && !hasChunkPrefix(d.ID, chunkPrefix) {
			return fmt.Errorf("document %q is neither %q nor one of its chunks", d.ID, base)
		}
		if keep[d.ID] {
			return fmt.Errorf("document %q is given twice", d.ID)
		}
		if err := validDocument(collectionName, d); err != nil {
			return err
		}
		unit, err := normalize(d.Embedding)
		if err != nil {
			return err
		}
		if len(prepared) > 0 && (d.Model != prepared[0].Model || len(unit) != len(prepared[0].Embedding)) {
			return fmt.Errorf("%w: the documents of %q hold embeddings of different models or dimensions", ErrModelMismatch, base)
		}
		d.Embedding = unit
		if d.CreatedAt.IsZero() {
			d.CreatedAt = now
		}
		keep[d.ID] = true
		prepared = append(prepared, d)
	}

	k.mu.Lock()
	defer k.mu.Unlock()
	c := k.cols[collectionName]
	var remove []string
	others := 0
	if c != nil {
		for id := range c.docs {
			switch {
			case keep[id]:
			case id == base || hasChunkPrefix(id, chunkPrefix):
				remove = append(remove, id)
			default:
				others++
			}
		}
		sort.Strings(remove)
		if others > 0 && len(prepared) > 0 && (prepared[0].Model != c.model || len(prepared[0].Embedding) != c.dims) {
			return fmt.Errorf("%w: collection %q holds %d-dimension embeddings of %q, not %d-dimension embeddings of %q",
				ErrModelMismatch, collectionName, c.dims, c.model, len(prepared[0].Embedding), prepared[0].Model)
		}
	}
	if others+len(prepared) > k.maxDocuments {
		return fmt.Errorf("collection %q is full (at most %d documents)", collectionName, k.maxDocuments)
	}
	if len(prepared) == 0 && len(remove) == 0 {
		return nil
	}
	if k.persist != nil {
		if err := k.persist.ReplaceLLMDocuments(collectionName, prepared, remove); err != nil {
			return err
		}
	}
	if c == nil {
		c = &collection{docs: map[string]*Document{}}
		k.cols[collectionName] = c
	}
	for _, id := range remove {
		delete(c.docs, id)
	}
	for i := range prepared {
		d := prepared[i]
		c.docs[d.ID] = &d
	}
	if len(c.docs) == 0 {
		delete(k.cols, collectionName)
		return nil
	}
	if len(prepared) > 0 {
		c.model, c.dims = prepared[0].Model, len(prepared[0].Embedding)
	}
	c.updated = now
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
	// best holds the topK best matches so far, best first (higher score,
	// then lower ID): a bounded selection, so a large collection is not
	// copied and sorted to return a few hits.
	best := make([]scoredDocument, 0, min(topK, len(c.docs)))
	for _, d := range c.docs {
		var dot float64
		for i, v := range d.Embedding {
			dot += float64(v) * float64(q[i])
		}
		if dot < minScore {
			continue
		}
		sd := scoredDocument{score: dot, doc: d}
		if len(best) == topK {
			if !sd.before(best[topK-1]) {
				continue
			}
			best = best[:topK-1]
		}
		i := sort.Search(len(best), func(i int) bool { return sd.before(best[i]) })
		best = append(best, scoredDocument{})
		copy(best[i+1:], best[i:])
		best[i] = sd
	}
	hits := make([]Hit, len(best))
	for i, sd := range best {
		hits[i] = Hit{Document: *sd.doc, Score: sd.score}
		hits[i].Embedding = nil
	}
	return hits, nil
}

// scoredDocument is a search match: a stored document and its score.
type scoredDocument struct {
	score float64
	doc   *Document
}

// before reports whether a ranks before b: a higher score, then a lower ID
// (IDs are unique in a collection, so the order is total).
func (a scoredDocument) before(b scoredDocument) bool {
	if a.score != b.score {
		return a.score > b.score
	}
	return a.doc.ID < b.doc.ID
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

// DeleteMatching removes the documents of a collection whose ID match
// accepts, each after its persistent deletion succeeds, and returns how
// many it removed (those before a failed deletion stay removed). It reads
// only the IDs: no document is copied.
func (k *Knowledge) DeleteMatching(collectionName string, match func(id string) bool) (int, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	c := k.cols[collectionName]
	if c == nil {
		return 0, nil
	}
	var ids []string
	for id := range c.docs {
		if match(id) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	removed := 0
	var err error
	for _, id := range ids {
		if k.persist != nil {
			if err = k.persist.DeleteLLMDocument(collectionName, id); err != nil {
				break
			}
		}
		delete(c.docs, id)
		removed++
	}
	if len(c.docs) == 0 {
		delete(k.cols, collectionName)
	} else if removed > 0 {
		c.updated = time.Now()
	}
	return removed, err
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

// Collection returns the summary of the collection named name; false when
// there is none. A collection never stays empty: it is removed with its
// last document.
func (k *Knowledge) Collection(name string) (CollectionInfo, bool) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	c := k.cols[name]
	if c == nil {
		return CollectionInfo{}, false
	}
	return CollectionInfo{Name: name, Documents: len(c.docs), Model: c.model, Dimensions: c.dims, UpdatedAt: c.updated}, true
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
	// Stored documents are never changed (a write stores a new one), so
	// their pointers are sorted outside the lock and only the page copied.
	all := make([]*Document, 0, len(c.docs))
	for _, d := range c.docs {
		all = append(all, d)
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
	page := make([]Document, 0, end-offset)
	for _, d := range all[offset:end] {
		doc := *d
		doc.Embedding = nil
		page = append(page, doc)
	}
	return page, total
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
