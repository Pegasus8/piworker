package llm

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

type memMemoryPersister struct {
	convs   map[string][]Turn
	deleted []string
}

func (p *memMemoryPersister) LoadLLMConversations() (map[string][]Turn, error) {
	out := map[string][]Turn{}
	for k, v := range p.convs {
		out[k] = append([]Turn(nil), v...)
	}
	return out, nil
}
func (p *memMemoryPersister) SaveLLMConversation(key string, turns []Turn) error {
	p.convs[key] = append([]Turn(nil), turns...)
	return nil
}
func (p *memMemoryPersister) DeleteLLMConversation(key string) error {
	delete(p.convs, key)
	p.deleted = append(p.deleted, key)
	return nil
}

func TestMemoryAppendKeepsTheLastTurns(t *testing.T) {
	p := &memMemoryPersister{convs: map[string][]Turn{
		"old": {{Role: RoleUser, Text: "hi", At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}},
	}}
	m := NewMemory()
	if err := m.Attach(p); err != nil {
		t.Fatal(err)
	}
	if got := m.Turns("old"); len(got) != 1 || got[0].Text != "hi" {
		t.Fatalf("loaded: %+v", got)
	}
	for i := 0; i < 5; i++ {
		err := m.Append("telegram:1", 4,
			Turn{Role: RoleUser, Text: fmt.Sprintf("q%d", i)},
			Turn{Role: RoleAssistant, Text: fmt.Sprintf("a%d", i)})
		if err != nil {
			t.Fatal(err)
		}
	}
	turns := m.Turns("telegram:1")
	if len(turns) != 4 || turns[0].Text != "q3" || turns[3].Text != "a4" {
		t.Fatalf("kept: %+v", turns)
	}
	if turns[0].At.IsZero() {
		t.Fatal("turn time not set")
	}
	if len(p.convs["telegram:1"]) != 4 {
		t.Fatal("not written through")
	}
	// Turns is a copy.
	turns[0].Text = "changed"
	if m.Turns("telegram:1")[0].Text != "q3" {
		t.Fatal("Turns returned the store's slice")
	}
	list := m.List()
	if len(list) != 2 || list[0].Key != "old" || list[1].Key != "telegram:1" || list[1].Turns != 4 {
		t.Fatalf("list: %+v", list)
	}
	if err := m.Clear("telegram:1"); err != nil {
		t.Fatal(err)
	}
	if len(m.Turns("telegram:1")) != 0 || p.convs["telegram:1"] != nil {
		t.Fatal("not cleared")
	}
}

func TestMemoryLimits(t *testing.T) {
	m := NewMemory()
	if err := m.Append("", 10, Turn{Role: RoleUser, Text: "x"}); err == nil {
		t.Error("empty key accepted")
	}
	if err := m.Append(strings.Repeat("k", MaxConversationKeyLength+1), 10, Turn{Role: RoleUser, Text: "x"}); err == nil {
		t.Error("long key accepted")
	}
	if err := m.Append("k", 10, Turn{Role: "system", Text: "x"}); err == nil {
		t.Error("bad role accepted")
	}
	// keep is capped at MaxTurns.
	var many []Turn
	for i := 0; i < MaxTurns+10; i++ {
		many = append(many, Turn{Role: RoleUser, Text: "t"})
	}
	if err := m.Append("k", 10000, many...); err != nil {
		t.Fatal(err)
	}
	if n := len(m.Turns("k")); n != MaxTurns {
		t.Fatalf("turns %d, want %d", n, MaxTurns)
	}
	// Long texts are trimmed.
	if err := m.Append("long", 2, Turn{Role: RoleUser, Text: strings.Repeat("é", MaxTurnBytes)}); err != nil {
		t.Fatal(err)
	}
	if n := len(m.Turns("long")[0].Text); n > MaxTurnBytes {
		t.Fatalf("text not trimmed: %d bytes", n)
	}
}

func TestMemoryEvictsTheLeastRecentlyUpdated(t *testing.T) {
	p := &memMemoryPersister{convs: map[string][]Turn{}}
	m := NewMemory()
	m.maxConversations = 3
	clock := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	m.now = func() time.Time { clock = clock.Add(time.Second); return clock }
	if err := m.Attach(p); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"a", "b", "c"} {
		if err := m.Append(k, 10, Turn{Role: RoleUser, Text: k}); err != nil {
			t.Fatal(err)
		}
	}
	// "a" is updated again: "b" is now the least recently updated.
	if err := m.Append("a", 10, Turn{Role: RoleAssistant, Text: "again"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Append("d", 10, Turn{Role: RoleUser, Text: "d"}); err != nil {
		t.Fatal(err)
	}
	if len(m.Turns("b")) != 0 || len(m.List()) != 3 {
		t.Fatalf("not evicted: %+v", m.List())
	}
	if _, ok := p.convs["b"]; ok {
		t.Fatal("eviction not written through")
	}
}

func TestMessagesFromTurns(t *testing.T) {
	turns := []Turn{
		{Role: RoleAssistant, Text: "orphan"},
		{Role: RoleUser, Text: "q1"},
		{Role: RoleUser, Text: "q1b"},
		{Role: RoleAssistant, Text: "a1"},
	}
	msgs := MessagesFromTurns(turns)
	if len(msgs) != 2 || msgs[0].Role != RoleUser || msgs[0].Text != "q1\n\nq1b" || msgs[1].Text != "a1" {
		t.Fatalf("messages: %+v", msgs)
	}
}

func TestMemoryContextIsolation(t *testing.T) {
	m := NewMemory()
	if err := m.Append("k", 10, Turn{Role: RoleUser, Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if MemoryFromContext(context.Background()) != DefaultMemory {
		t.Fatal("default memory")
	}
	if MemoryFromContext(WithMemory(context.Background(), m)) != m {
		t.Fatal("memory from context")
	}
	isolated := MemoryFromContext(WithMemory(context.Background(), nil))
	if isolated == DefaultMemory || len(isolated.Turns("k")) != 0 {
		t.Fatal("nil memory must be an empty one")
	}
}
