package llm

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"unicode/utf8"
)

type memKnowledgePersister struct {
	docs map[string]map[string]Document
}

func (p *memKnowledgePersister) LoadLLMDocuments() (map[string][]Document, error) {
	out := map[string][]Document{}
	for c, docs := range p.docs {
		for _, d := range docs {
			out[c] = append(out[c], d)
		}
	}
	return out, nil
}
func (p *memKnowledgePersister) SaveLLMDocument(collection string, d Document) error {
	if p.docs[collection] == nil {
		p.docs[collection] = map[string]Document{}
	}
	p.docs[collection][d.ID] = d
	return nil
}
func (p *memKnowledgePersister) DeleteLLMDocument(collection, id string) error {
	delete(p.docs[collection], id)
	return nil
}
func (p *memKnowledgePersister) DeleteLLMCollection(collection string) error {
	delete(p.docs, collection)
	return nil
}

func TestKnowledgeAddAndSearch(t *testing.T) {
	p := &memKnowledgePersister{docs: map[string]map[string]Document{
		"faq": {"seed": {ID: "seed", Text: "seed", Model: "emb", Embedding: Embedding{0, 0, 3}}},
	}}
	k := NewKnowledge()
	if err := k.Attach(p); err != nil {
		t.Fatal(err)
	}
	add := func(id string, e Embedding) {
		t.Helper()
		if err := k.Add("faq", Document{ID: id, Text: "text " + id, Model: "emb", Embedding: e, Metadata: map[string]any{"id": id}}); err != nil {
			t.Fatal(err)
		}
	}
	add("x", Embedding{2, 0, 0})
	add("xy", Embedding{1, 1, 0})
	add("y", Embedding{0, 5, 0})

	stored := p.docs["faq"]["x"].Embedding
	if math.Abs(float64(stored[0])-1) > 1e-6 {
		t.Fatalf("embedding not normalized: %v", stored)
	}

	hits, err := k.Search("faq", "emb", Embedding{1, 0, 0}, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].ID != "x" || hits[1].ID != "xy" || math.Abs(hits[0].Score-1) > 1e-6 {
		t.Fatalf("hits: %+v", hits)
	}
	if hits[0].Metadata["id"] != "x" || hits[0].Text != "text x" {
		t.Fatalf("hit document: %+v", hits[0])
	}
	// minScore filters.
	hits, _ = k.Search("faq", "emb", Embedding{1, 0, 0}, 10, 0.5)
	if len(hits) != 2 {
		t.Fatalf("minScore: %+v", hits)
	}
	// An unknown collection has no hits.
	if hits, err := k.Search("none", "emb", Embedding{1, 0, 0}, 3, 0); err != nil || len(hits) != 0 {
		t.Fatalf("unknown collection: %v %v", hits, err)
	}
	// Another model, or other dimensions, do not match.
	if _, err := k.Search("faq", "other", Embedding{1, 0, 0}, 3, 0); !errors.Is(err, ErrModelMismatch) {
		t.Fatalf("model mismatch: %v", err)
	}
	if _, err := k.Search("faq", "emb", Embedding{1, 0}, 3, 0); !errors.Is(err, ErrModelMismatch) {
		t.Fatalf("dimensions mismatch: %v", err)
	}
	if err := k.Add("faq", Document{ID: "z", Text: "z", Model: "other", Embedding: Embedding{1, 0, 0}}); !errors.Is(err, ErrModelMismatch) {
		t.Fatalf("add with another model: %v", err)
	}
	if err := k.Add("faq", Document{ID: "z", Text: "z", Model: "emb", Embedding: Embedding{1, 0}}); !errors.Is(err, ErrModelMismatch) {
		t.Fatalf("add with other dimensions: %v", err)
	}

	// Replacing keeps one document per ID.
	add("x", Embedding{0, 1, 0})
	cols := k.Collections()
	if len(cols) != 1 || cols[0].Name != "faq" || cols[0].Documents != 4 || cols[0].Model != "emb" || cols[0].Dimensions != 3 {
		t.Fatalf("collections: %+v", cols)
	}
	docs, total := k.Documents("faq", 1, 2)
	if total != 4 || len(docs) != 2 {
		t.Fatalf("documents page: %d %+v", total, docs)
	}
	if docs[0].Embedding != nil {
		t.Fatal("Documents returned embeddings")
	}

	if err := k.Delete("faq", "x"); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.docs["faq"]["x"]; ok {
		t.Fatal("delete not written through")
	}
	if err := k.DeleteCollection("faq"); err != nil {
		t.Fatal(err)
	}
	if len(k.Collections()) != 0 || p.docs["faq"] != nil {
		t.Fatal("collection not deleted")
	}
	// An emptied collection takes any model again.
	if err := k.Add("faq", Document{ID: "n", Text: "n", Model: "new", Embedding: Embedding{1, 2}}); err != nil {
		t.Fatal(err)
	}
}

func TestKnowledgeLimits(t *testing.T) {
	k := NewKnowledge()
	ok := Document{ID: "a", Text: "t", Model: "m", Embedding: Embedding{1}}
	bad := map[string]struct {
		collection string
		doc        Document
	}{
		"collection": {"bad name", ok},
		"no id":      {"c", Document{Text: "t", Model: "m", Embedding: Embedding{1}}},
		"long id":    {"c", Document{ID: strings.Repeat("i", 201), Text: "t", Model: "m", Embedding: Embedding{1}}},
		"long text":  {"c", Document{ID: "a", Text: strings.Repeat("t", MaxDocumentBytes+1), Model: "m", Embedding: Embedding{1}}},
		"no model":   {"c", Document{ID: "a", Text: "t", Embedding: Embedding{1}}},
		"empty":      {"c", Document{ID: "a", Text: "t", Model: "m"}},
		"zero":       {"c", Document{ID: "a", Text: "t", Model: "m", Embedding: Embedding{0, 0}}},
		"nan":        {"c", Document{ID: "a", Text: "t", Model: "m", Embedding: Embedding{float32(math.NaN())}}},
		"dims":       {"c", Document{ID: "a", Text: "t", Model: "m", Embedding: make(Embedding, MaxDimensions+1)}},
		"metadata":   {"c", Document{ID: "a", Text: "t", Model: "m", Embedding: Embedding{1}, Metadata: map[string]any{"x": strings.Repeat("m", 9000)}}},
	}
	for name, c := range bad {
		if err := k.Add(c.collection, c.doc); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	k.maxDocuments = 2
	for i := 0; i < 2; i++ {
		d := ok
		d.ID = fmt.Sprint(i)
		if err := k.Add("c", d); err != nil {
			t.Fatal(err)
		}
	}
	d := ok
	d.ID = "third"
	if err := k.Add("c", d); err == nil {
		t.Fatal("collection over its limit")
	}
	d.ID = "1" // replacing is fine
	if err := k.Add("c", d); err != nil {
		t.Fatal(err)
	}
}

func TestKnowledgeContextIsolation(t *testing.T) {
	k := NewKnowledge()
	if KnowledgeFromContext(context.Background()) != DefaultKnowledge {
		t.Fatal("default knowledge")
	}
	if KnowledgeFromContext(WithKnowledge(context.Background(), k)) != k {
		t.Fatal("knowledge from context")
	}
	if got := KnowledgeFromContext(WithKnowledge(context.Background(), nil)); got == DefaultKnowledge || got == nil {
		t.Fatal("nil knowledge must be an empty one")
	}
}

func TestChunk(t *testing.T) {
	if got := Chunk("  short text ", 100); len(got) != 1 || got[0] != "short text" {
		t.Fatalf("short: %q", got)
	}
	if got := Chunk("   ", 100); len(got) != 0 {
		t.Fatalf("blank: %q", got)
	}
	var paragraphs []string
	for p := 0; p < 6; p++ {
		var sentences []string
		for s := 0; s < 5; s++ {
			sentences = append(sentences, fmt.Sprintf("Paragraph %d sentence %d has a few words in it.", p, s))
		}
		paragraphs = append(paragraphs, strings.Join(sentences, " "))
	}
	text := strings.Join(paragraphs, "\n\n") + "\n\n" + strings.Repeat("x", 450) + " end"
	const size = 200
	chunks := Chunk(text, size)
	if len(chunks) < 5 {
		t.Fatalf("too few chunks: %d", len(chunks))
	}
	for i, c := range chunks {
		if n := utf8.RuneCountInString(c); n > size || n == 0 {
			t.Errorf("chunk %d has %d runes", i, n)
		}
	}
	// Every word of the text is in some chunk, in order.
	joined := strings.Join(chunks, " ")
	for _, w := range []string{"Paragraph 0 sentence 0", "Paragraph 5 sentence 4", "end"} {
		if !strings.Contains(joined, w) {
			t.Errorf("lost %q", w)
		}
	}
	// Consecutive chunks overlap (10 %): the start of a chunk repeats the end of the previous one.
	overlaps := 0
	for i := 1; i < len(chunks); i++ {
		first := strings.Fields(chunks[i])[0]
		if strings.Contains(chunks[i-1], first) {
			overlaps++
		}
	}
	if overlaps == 0 {
		t.Fatal("no overlap between chunks")
	}
	// A single text paragraph is split on sentences before words.
	sent := Chunk(paragraphs[0], 120)
	for _, c := range sent[:len(sent)-1] {
		if !strings.HasSuffix(c, ".") {
			t.Errorf("chunk does not end on a sentence: %q", c)
		}
	}
}

// DeleteMatching removes the documents whose ID the function accepts,
// written through, and an emptied collection with them.
func TestKnowledgeDeleteMatching(t *testing.T) {
	p := &memKnowledgePersister{docs: map[string]map[string]Document{}}
	k := NewKnowledge()
	if err := k.Attach(p); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"manual", "manual#1", "manual#2", "manual#3", "other#1"} {
		if err := k.Add("docs", Document{ID: id, Text: id, Model: "emb", Embedding: Embedding{1, 0}}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := k.DeleteMatching("docs", func(id string) bool { return id == "manual" || id == "manual#3" })
	if err != nil || n != 2 {
		t.Fatalf("DeleteMatching: %d %v", n, err)
	}
	docs, total := k.Documents("docs", 0, 10)
	if total != 3 || docs[0].ID != "manual#1" || docs[1].ID != "manual#2" || docs[2].ID != "other#1" {
		t.Fatalf("left: %+v", docs)
	}
	if _, ok := p.docs["docs"]["manual#3"]; ok {
		t.Fatal("not written through")
	}
	if n, err := k.DeleteMatching("none", func(string) bool { return true }); err != nil || n != 0 {
		t.Fatalf("unknown collection: %d %v", n, err)
	}
	if n, err := k.DeleteMatching("docs", func(string) bool { return true }); err != nil || n != 3 {
		t.Fatalf("all: %d %v", n, err)
	}
	if len(k.Collections()) != 0 {
		t.Fatal("an emptied collection is removed")
	}
}
