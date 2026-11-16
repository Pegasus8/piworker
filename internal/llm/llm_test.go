package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/secrets"
)

func TestConnectionValidate(t *testing.T) {
	ok := []Connection{
		{Name: "claude", Provider: ProviderAnthropic, APIKey: "{{secret.ANTHROPIC}}", Model: "claude-opus-5-5"},
		{Name: "gpt", Provider: ProviderOpenAI, APIKey: "sk-x"},
		{Name: "ollama", Provider: ProviderOpenAI, BaseURL: "http://192.168.1.20:11434/v1"},
		{Name: "a_b-1", Provider: ProviderAnthropic, APIKey: "k", BaseURL: "https://proxy.example.com/anthropic", DailyTokenLimit: 1000},
	}
	for _, c := range ok {
		c.Normalize()
		if err := c.Validate(); err != nil {
			t.Errorf("%+v: unexpected error %v", c, err)
		}
	}
	bad := map[string]Connection{
		"name":           {Name: "bad name", Provider: ProviderOpenAI},
		"provider":       {Name: "x", Provider: "gemini"},
		"scheme":         {Name: "x", Provider: ProviderOpenAI, BaseURL: "ftp://host"},
		"userinfo":       {Name: "x", Provider: ProviderOpenAI, BaseURL: "https://u:p@host/v1"},
		"query":          {Name: "x", Provider: ProviderOpenAI, BaseURL: "https://host/v1?key=1"},
		"no host":        {Name: "x", Provider: ProviderOpenAI, BaseURL: "https:///v1"},
		"negative limit": {Name: "x", Provider: ProviderOpenAI, DailyTokenLimit: -1},
		"anthropic key":  {Name: "x", Provider: ProviderAnthropic},
	}
	for what, c := range bad {
		c.Normalize()
		if err := c.Validate(); err == nil {
			t.Errorf("%s: expected an error", what)
		} else if strings.Contains(err.Error(), "sk-") {
			t.Errorf("%s: error quotes the key: %v", what, err)
		}
	}
}

func TestConnectionNormalizeDefaults(t *testing.T) {
	c := Connection{Name: " gpt ", Provider: " OpenAI ", APIKey: " k "}
	c.Normalize()
	if c.Name != "gpt" || c.Provider != ProviderOpenAI || c.APIKey != "k" {
		t.Fatalf("normalized: %+v", c)
	}
	if c.Endpoint() != "https://api.openai.com/v1" {
		t.Fatalf("default OpenAI endpoint: %q", c.Endpoint())
	}
	a := Connection{Name: "a", Provider: ProviderAnthropic, APIKey: "k"}
	a.Normalize()
	if a.Endpoint() != "https://api.anthropic.com" {
		t.Fatalf("default Anthropic endpoint: %q", a.Endpoint())
	}
}

type memPersister struct {
	conns map[string]Connection
	usage map[string]Usage
}

func (p *memPersister) LoadLLMConnections() (map[string]Connection, error) {
	out := map[string]Connection{}
	for k, v := range p.conns {
		out[k] = v
	}
	return out, nil
}
func (p *memPersister) SaveLLMConnection(c Connection) error { p.conns[c.Name] = c; return nil }
func (p *memPersister) DeleteLLMConnection(name string) error {
	delete(p.conns, name)
	return nil
}
func (p *memPersister) AddLLMUsage(day, name string, u Usage) error {
	k := day + "/" + name
	cur := p.usage[k]
	cur.InputTokens += u.InputTokens
	cur.OutputTokens += u.OutputTokens
	cur.Requests += u.Requests
	p.usage[k] = cur
	return nil
}
func (p *memPersister) LLMUsage(day, name string) (Usage, error) { return p.usage[day+"/"+name], nil }

func TestStoreWritesThroughAndLooksUpByContext(t *testing.T) {
	p := &memPersister{conns: map[string]Connection{"old": {Name: "old", Provider: ProviderOpenAI}}, usage: map[string]Usage{}}
	s := NewStore()
	if err := s.Attach(p); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("old"); !ok {
		t.Fatal("loaded connection missing")
	}
	if err := s.Set(Connection{Name: "gpt", Provider: ProviderOpenAI, APIKey: "k"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.conns["gpt"]; !ok {
		t.Fatal("not written through")
	}
	if err := s.Set(Connection{Name: "bad", Provider: "x"}); err == nil {
		t.Fatal("invalid connection stored")
	}
	if got := s.Names(); strings.Join(got, ",") != "gpt,old" {
		t.Fatalf("names %v", got)
	}

	ctx := WithStore(context.Background(), s)
	if _, err := Lookup(ctx, "gpt"); err != nil {
		t.Fatal(err)
	}
	// An isolated (sandboxed) context sees an empty store.
	if _, err := Lookup(WithStore(context.Background(), nil), "gpt"); !errors.Is(err, ErrUndefined) {
		t.Fatalf("isolated lookup: %v", err)
	}
	if err := s.Delete("gpt"); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.conns["gpt"]; ok {
		t.Fatal("delete not written through")
	}
}

func TestStoreUsageAndDailyLimit(t *testing.T) {
	p := &memPersister{conns: map[string]Connection{}, usage: map[string]Usage{}}
	s := NewStore()
	now := time.Date(2026, 11, 15, 10, 0, 0, 0, time.Local)
	s.now = func() time.Time { return now }
	if err := s.Attach(p); err != nil {
		t.Fatal(err)
	}
	c := Connection{Name: "gpt", Provider: ProviderOpenAI, DailyTokenLimit: 100}
	if err := s.Set(c); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckBudget(c); err != nil {
		t.Fatalf("fresh budget: %v", err)
	}
	if err := s.RecordUsage("gpt", Usage{InputTokens: 60, OutputTokens: 50, Requests: 1}); err != nil {
		t.Fatal(err)
	}
	if u := s.UsageToday("gpt"); u.InputTokens != 60 || u.OutputTokens != 50 || u.Requests != 1 {
		t.Fatalf("usage %+v", u)
	}
	err := s.CheckBudget(c)
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("over the limit: %v", err)
	}
	// A new day starts afresh.
	now = now.Add(24 * time.Hour)
	if err := s.CheckBudget(c); err != nil {
		t.Fatalf("next day: %v", err)
	}
	// No limit: never exhausted.
	if err := s.CheckBudget(Connection{Name: "gpt"}); err != nil {
		t.Fatal(err)
	}
}

func TestSecretReferences(t *testing.T) {
	refs := SecretReferences(Connection{APIKey: "{{secret.A}}-{{secret.B}}"})
	if len(refs) != 2 || refs[0].Name != "A" || refs[1].Name != "B" || refs[0].Setting != "apiKey" {
		t.Fatalf("refs %+v", refs)
	}
}

func TestResolvedKeyNeedsItsSecret(t *testing.T) {
	store := secrets.NewStore()
	_ = store.Set("KEY", "sk-real")
	ctx := secrets.WithStore(context.Background(), store)
	key, err := Connection{Name: "x", APIKey: "{{secret.KEY}}"}.resolvedKey(ctx)
	if err != nil || key != "sk-real" {
		t.Fatalf("key %q err %v", key, err)
	}
	if _, err := (Connection{Name: "x", APIKey: "{{secret.MISSING}}"}).resolvedKey(ctx); err == nil {
		t.Fatal("undefined secret accepted")
	}
}
