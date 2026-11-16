package llm

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Memory limits.
const (
	// MaxConversationKeyLength bounds a conversation key, in characters.
	MaxConversationKeyLength = 200
	// MaxTurns bounds the turns kept per conversation.
	MaxTurns = 200
	// MaxTurnBytes bounds the text of one stored turn (longer texts are cut).
	MaxTurnBytes = 32 << 10
	// MaxConversations bounds the conversations kept: adding one past it
	// evicts the least recently updated.
	MaxConversations = 5000
)

// MemoryPersister persists conversations and provides the initial load. The
// SQLite store implements it.
type MemoryPersister interface {
	LoadLLMConversations() (map[string][]Turn, error)
	SaveLLMConversation(key string, turns []Turn) error
	DeleteLLMConversation(key string) error
}

// Memory is a thread-safe store of conversations (key → turns) with
// optional persistence. Only text turns are kept: no images, no tool calls.
type Memory struct {
	mu      sync.Mutex
	convs   map[string]*conversation
	persist MemoryPersister

	maxConversations int              // MaxConversations (tests lower it)
	now              func() time.Time // the clock (tests replace it)
	// lastGen is the newest generation, of a conversation started or of a
	// Clear: generations only grow.
	lastGen uint64
	// cleared holds, for clearedLifetime, the generation at which each
	// key was cleared: an exchange that read the key before it
	// (AppendAfter) is dropped, even when there was no conversation yet.
	cleared map[string]clearMark
}

// clearedLifetime is how long Clear's mark of a key is kept: longer than
// any exchange takes (a node's timeout is at most 10 minutes).
const clearedLifetime = time.Hour

// clearMark is when (gen, at) a key was cleared.
type clearMark struct {
	gen uint64
	at  time.Time
}

// conversation is a stored conversation. gen tells it from another
// conversation of the same key: it is set when the conversation starts and
// kept by every append, so one cleared (or evicted) and started again has
// another.
type conversation struct {
	turns   []Turn
	updated time.Time
	gen     uint64
}

// NewMemory creates an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{convs: map[string]*conversation{}, cleared: map[string]clearMark{}, maxConversations: MaxConversations, now: time.Now}
}

// ValidConversationKey reports whether key is a legal conversation key: 1
// to MaxConversationKeyLength characters of valid UTF-8, one line.
func ValidConversationKey(key string) bool {
	return key != "" && utf8.ValidString(key) && utf8.RuneCountInString(key) <= MaxConversationKeyLength &&
		!strings.ContainsAny(key, "\x00\r\n")
}

// Attach loads the persisted conversations and enables write-through.
func (m *Memory) Attach(p MemoryPersister) error {
	loaded, err := p.LoadLLMConversations()
	if err != nil {
		return fmt.Errorf("failed to load LLM conversations: %w", err)
	}
	convs := make(map[string]*conversation, len(loaded))
	m.mu.Lock()
	for key, turns := range loaded {
		if len(turns) == 0 {
			continue
		}
		m.lastGen++
		convs[key] = &conversation{turns: turns, updated: turns[len(turns)-1].At, gen: m.lastGen}
	}
	m.convs = convs
	m.persist = p
	m.mu.Unlock()
	return nil
}

// Turns returns a copy of the turns of key (nil when there are none).
func (m *Memory) Turns(key string) []Turn {
	turns, _ := m.Read(key)
	return turns
}

// Read returns a copy of the turns of key (nil when there are none) and
// the generation it read them at, for AppendAfter.
func (m *Memory) Read(key string) ([]Turn, uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.convs[key]
	if c == nil {
		return nil, m.lastGen
	}
	return append([]Turn(nil), c.turns...), c.gen
}

// Append adds turns to the conversation key and keeps its last keep turns
// (at most MaxTurns; keep ≤ 0 means MaxTurns), writing through first. Each
// text is cut to MaxTurnBytes; a turn without a time gets the current one.
// A new conversation past MaxConversations evicts the least recently
// updated one.
func (m *Memory) Append(key string, keep int, turns ...Turn) error {
	if !ValidConversationKey(key) {
		return fmt.Errorf("invalid conversation key (1 to %d characters, one line)", MaxConversationKeyLength)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.appendLocked(key, keep, turns)
}

// AppendAfter is Append for an exchange that read the conversation key at
// generation gen (Read) before asking the model: when key was cleared
// since (by a Forget node or the AI memory dialog), the turns are dropped
// and ok is false, so a conversation forgotten while the model answered
// never comes back with the exchange, nor starts with it when there was
// none yet. Otherwise the turns are appended to the conversation as it is
// now (another message may have started it, or added to it, meanwhile).
func (m *Memory) AppendAfter(key string, gen uint64, keep int, turns ...Turn) (ok bool, err error) {
	if !ValidConversationKey(key) {
		return false, fmt.Errorf("invalid conversation key (1 to %d characters, one line)", MaxConversationKeyLength)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if mark, cleared := m.cleared[key]; cleared && mark.gen > gen {
		return false, nil
	}
	if err := m.appendLocked(key, keep, turns); err != nil {
		return false, err
	}
	return true, nil
}

// appendLocked is Append with m.mu held.
func (m *Memory) appendLocked(key string, keep int, turns []Turn) error {
	if keep <= 0 || keep > MaxTurns {
		keep = MaxTurns
	}
	now := m.now()
	add := make([]Turn, 0, len(turns))
	for _, t := range turns {
		if t.Role != RoleUser && t.Role != RoleAssistant {
			return fmt.Errorf("invalid turn role %q (user or assistant)", t.Role)
		}
		t.Text = truncate(t.Text, MaxTurnBytes)
		if t.At.IsZero() {
			t.At = now
		}
		add = append(add, t)
	}
	c := m.convs[key]
	var next []Turn
	if c != nil {
		next = append(next, c.turns...)
	}
	next = append(next, add...)
	if len(next) > keep {
		next = next[len(next)-keep:]
	}
	next = append([]Turn(nil), next...) // never share the old backing array

	if c == nil && len(m.convs) >= m.maxConversations {
		if err := m.evictLocked(); err != nil {
			return err
		}
	}
	if m.persist != nil {
		if err := m.persist.SaveLLMConversation(key, next); err != nil {
			return err
		}
	}
	var gen uint64
	if c != nil {
		gen = c.gen
	} else {
		m.lastGen++
		gen = m.lastGen
	}
	m.convs[key] = &conversation{turns: next, updated: now, gen: gen}
	return nil
}

// evictLocked removes the least recently updated conversation (m.mu held).
func (m *Memory) evictLocked() error {
	oldest := ""
	var at time.Time
	for key, c := range m.convs {
		if oldest == "" || c.updated.Before(at) || (c.updated.Equal(at) && key < oldest) {
			oldest, at = key, c.updated
		}
	}
	if oldest == "" {
		return nil
	}
	if m.persist != nil {
		if err := m.persist.DeleteLLMConversation(oldest); err != nil {
			return fmt.Errorf("failed to evict the oldest conversation: %w", err)
		}
	}
	delete(m.convs, oldest)
	return nil
}

// Clear deletes the conversation key after its persistent deletion
// succeeds. Exchanges in flight that read key before (AppendAfter) are
// not saved, whether or not it had a conversation.
func (m *Memory) Clear(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.persist != nil {
		if err := m.persist.DeleteLLMConversation(key); err != nil {
			return err
		}
	}
	delete(m.convs, key)
	now := m.now()
	for k, mark := range m.cleared {
		if now.Sub(mark.at) > clearedLifetime {
			delete(m.cleared, k)
		}
	}
	m.lastGen++
	m.cleared[key] = clearMark{gen: m.lastGen, at: now}
	return nil
}

// List summarizes the conversations, sorted by key.
func (m *Memory) List() []ConversationInfo {
	m.mu.Lock()
	out := make([]ConversationInfo, 0, len(m.convs))
	for key, c := range m.convs {
		out = append(out, ConversationInfo{Key: key, Turns: len(c.turns), UpdatedAt: c.updated})
	}
	m.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// MessagesFromTurns turns a stored conversation into request messages that
// alternate between user and assistant, as providers expect: leading
// assistant turns are dropped and consecutive turns of one role are joined.
func MessagesFromTurns(turns []Turn) []Message {
	var out []Message
	for _, t := range turns {
		if len(out) == 0 && t.Role != RoleUser {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Role == t.Role {
			out[n-1].Text += "\n\n" + t.Text
			continue
		}
		out = append(out, Message{Role: t.Role, Text: t.Text})
	}
	return out
}

// DefaultMemory is the process-wide memory the nodes use.
var DefaultMemory = NewMemory()

type memoryKey struct{}

// WithMemory returns a context whose nodes use m instead of DefaultMemory;
// a nil m means an empty one (isolation fails closed).
func WithMemory(ctx context.Context, m *Memory) context.Context {
	if m == nil {
		m = NewMemory()
	}
	return context.WithValue(ctx, memoryKey{}, m)
}

// MemoryFromContext returns the memory carried by ctx, or DefaultMemory.
func MemoryFromContext(ctx context.Context) *Memory {
	if ctx != nil {
		if m, ok := ctx.Value(memoryKey{}).(*Memory); ok && m != nil {
			return m
		}
	}
	return DefaultMemory
}
