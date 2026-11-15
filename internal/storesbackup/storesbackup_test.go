package storesbackup

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/vars"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const passphrase = "correct horse battery staple"

// fixture is an installation's stores: global variables, two flows (one
// with variables), two secrets, two broker connections (with passwords) and
// three LLM connections (an Anthropic one with a key, a local one without,
// one whose key references a secret).
func fixture(t *testing.T) Sources {
	t.Helper()
	global := vars.NewStore()
	require.NoError(t, global.Set("threshold", 21.5))
	require.NoError(t, global.Set("room", map[string]any{"name": "kitchen"}))
	flowVars := map[string]*vars.Store{"f1": vars.NewStore(), "f2": vars.NewStore()}
	require.NoError(t, flowVars["f1"].Set("count", float64(3)))
	sec := secrets.NewStore()
	require.NoError(t, sec.Set("api_key", "zzSECRETzzAPIkey-value"))
	require.NoError(t, sec.Set("mqtt_pw", "zzSECRETzzMQTT"))
	conns := brokers.NewStore()
	require.NoError(t, conns.Set(brokers.Connection{Name: "home", Broker: "tcp://10.0.0.2:1883", Username: "pi", Password: "zzSECRETzzBROKER", BaseTopic: "z2m"}))
	require.NoError(t, conns.Set(brokers.Connection{Name: "lab", Broker: "tcp://10.0.0.3:1883", Password: "{{secret.mqtt_pw}}"}))
	models := llm.NewStore()
	require.NoError(t, models.Set(llm.Connection{Name: "claude", Provider: llm.ProviderAnthropic, APIKey: "zzSECRETzzANTHROPIC", Model: "claude-sonnet-4-5", DailyTokenLimit: 100000}))
	require.NoError(t, models.Set(llm.Connection{Name: "ollama", Provider: llm.ProviderOpenAI, BaseURL: "http://10.0.0.5:11434/v1", Model: "llama3.2"}))
	require.NoError(t, models.Set(llm.Connection{Name: "router", Provider: llm.ProviderOpenAI, BaseURL: "https://openrouter.ai/api/v1", APIKey: "{{secret.api_key}}"}))
	return Sources{
		Globals: global, Secrets: sec, Connections: conns, LLMConnections: models,
		Flows: []FlowStore{{ID: "f1", Name: "Heating", Vars: flowVars["f1"]}, {ID: "f2", Name: "Empty", Vars: flowVars["f2"]}},
	}
}

// Without a passphrase the export carries variables, the connections
// without passwords and the secret names only: no secret value, no
// password, in any form.
func TestExportWithoutPassphraseCarriesNoSecretValues(t *testing.T) {
	b, err := Export(fixture(t), "")
	require.NoError(t, err)
	assert.Equal(t, Format, b.Format)
	assert.Equal(t, Version, b.Version)
	assert.Equal(t, map[string]any{"threshold": 21.5, "room": map[string]any{"name": "kitchen"}}, b.Variables)
	assert.Equal(t, []FlowVariables{{FlowID: "f1", FlowName: "Heating", Variables: map[string]any{"count": float64(3)}}}, b.FlowVariables)
	assert.Equal(t, []Connection{
		{Name: "home", Broker: "tcp://10.0.0.2:1883", Username: "pi", BaseTopic: "z2m", HasPassword: true},
		{Name: "lab", Broker: "tcp://10.0.0.3:1883", BaseTopic: brokers.DefaultBaseTopic, HasPassword: true},
	}, b.Connections)
	assert.Equal(t, []string{"api_key", "mqtt_pw"}, b.Secrets)
	assert.Nil(t, b.Sealed)
	raw, err := json.Marshal(b)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "zzSECRETzz")
}

// With a passphrase the secret values and the whole connections (with
// their passwords) are sealed: none of them appears in the file, and only
// the passphrase opens them.
func TestExportWithPassphraseSealsSecretsAndConnections(t *testing.T) {
	b, err := Export(fixture(t), passphrase)
	require.NoError(t, err)
	require.NotNil(t, b.Sealed)
	assert.Empty(t, b.Connections, "connections travel sealed, so a password cannot be redirected to another broker")
	raw, err := json.Marshal(b)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "zzSECRETzz")
	assert.NotContains(t, string(raw), "10.0.0.2", "the sealed connections' brokers are not readable either")

	content, err := b.Sealed.Open(passphrase)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"api_key": "zzSECRETzzAPIkey-value", "mqtt_pw": "zzSECRETzzMQTT"}, content.Secrets)
	require.Len(t, content.Connections, 2)
	assert.Equal(t, "zzSECRETzzBROKER", content.Connections[0].Password)

	_, err = b.Sealed.Open("wrong passphrase, long enough")
	assert.ErrorIs(t, err, ErrWrongPassphrase)
	tampered := *b.Sealed
	tampered.Data = strings.Repeat("A", len(tampered.Data))
	_, err = tampered.Open(passphrase)
	assert.ErrorIs(t, err, ErrWrongPassphrase)
}

func TestExportRefusesAShortPassphrase(t *testing.T) {
	_, err := Export(fixture(t), "short")
	assert.ErrorIs(t, err, ErrWeakPassphrase)
}

// emptyDestination is a fresh installation with the flows "Heating" (a new
// ID, as an import of the flow gives it) and "Other".
func emptyDestination() (Sources, map[string]*vars.Store) {
	flowVars := map[string]*vars.Store{"n1": vars.NewStore(), "n2": vars.NewStore()}
	return Sources{
		Globals: vars.NewStore(), Secrets: secrets.NewStore(), Connections: brokers.NewStore(), LLMConnections: llm.NewStore(),
		Flows: []FlowStore{{ID: "n1", Name: "Heating", Vars: flowVars["n1"]}, {ID: "n2", Name: "Other", Vars: flowVars["n2"]}},
	}, flowVars
}

// Restoring a sealed export into a fresh installation recreates every
// store; flow variables follow their flow by ID, or by its unique name on
// another installation.
func TestRestoreRecreatesTheStores(t *testing.T) {
	b, err := Export(fixture(t), passphrase)
	require.NoError(t, err)
	dst, flowVars := emptyDestination()

	rep, err := Restore(b, passphrase, dst, Options{})
	require.NoError(t, err)
	v, _ := dst.Globals.Get("threshold")
	assert.Equal(t, 21.5, v)
	count, _ := flowVars["n1"].Get("count")
	assert.Equal(t, float64(3), count)
	s, _ := dst.Secrets.Get("api_key")
	assert.Equal(t, "zzSECRETzzAPIkey-value", s)
	c, ok := dst.Connections.Get("home")
	require.True(t, ok)
	assert.Equal(t, "zzSECRETzzBROKER", c.Password)
	claude, ok := dst.LLMConnections.Get("claude")
	require.True(t, ok)
	assert.Equal(t, llm.Connection{Name: "claude", Provider: llm.ProviderAnthropic, APIKey: "zzSECRETzzANTHROPIC", Model: "claude-sonnet-4-5", DailyTokenLimit: 100000}, claude)
	router, _ := dst.LLMConnections.Get("router")
	assert.Equal(t, "{{secret.api_key}}", router.APIKey)
	assert.Equal(t, Report{
		Variables:            Counts{Added: 2},
		FlowVariables:        Counts{Added: 1},
		Secrets:              Counts{Added: 2},
		Connections:          Counts{Added: 2},
		LLMConnections:       Counts{Added: 3},
		ChangedConnections:   []string{"home", "lab"},
		UnmatchedFlows:       []FlowRef{},
		SecretsNotRestored:   []string{},
		PasswordsNotRestored: []string{},
		APIKeysNotRestored:   []string{},
	}, rep, "LLM connections restart nothing: they are not in ChangedConnections")
}

// By default what exists is kept (skipped); with Overwrite it is replaced.
// Without the passphrase only the plain part is restored and the secret
// names are reported as not restored.
func TestRestoreKeepsOrOverwritesAndNeedsThePassphraseForSecrets(t *testing.T) {
	b, err := Export(fixture(t), passphrase)
	require.NoError(t, err)
	dst, _ := emptyDestination()
	require.NoError(t, dst.Globals.Set("threshold", 99.0))
	require.NoError(t, dst.Secrets.Set("api_key", "local-value-kept"))

	rep, err := Restore(b, "", dst, Options{})
	require.NoError(t, err)
	v, _ := dst.Globals.Get("threshold")
	assert.Equal(t, 99.0, v, "kept")
	assert.Equal(t, Counts{Added: 1, Skipped: 1}, rep.Variables)
	assert.Equal(t, []string{"api_key", "mqtt_pw"}, rep.SecretsNotRestored)
	assert.Equal(t, Counts{}, rep.Connections, "sealed connections need the passphrase")
	assert.Equal(t, Counts{}, rep.LLMConnections, "sealed LLM connections need the passphrase")
	s, _ := dst.Secrets.Get("api_key")
	assert.Equal(t, "local-value-kept", s)

	_, err = Restore(b, "wrong passphrase, long enough", dst, Options{})
	assert.ErrorIs(t, err, ErrWrongPassphrase)

	rep, err = Restore(b, passphrase, dst, Options{Overwrite: true})
	require.NoError(t, err)
	v, _ = dst.Globals.Get("threshold")
	assert.Equal(t, 21.5, v, "replaced")
	assert.Equal(t, Counts{Replaced: 2}, rep.Variables)
	assert.Equal(t, Counts{Added: 1, Replaced: 1}, rep.Secrets)
	s, _ = dst.Secrets.Get("api_key")
	assert.Equal(t, "zzSECRETzzAPIkey-value", s)
}

// Flow variables of a flow the installation does not have (by ID or by a
// unique name) are reported, not guessed.
func TestRestoreReportsUnmatchedFlows(t *testing.T) {
	b, err := Export(fixture(t), "")
	require.NoError(t, err)
	b.FlowVariables = append(b.FlowVariables, FlowVariables{FlowID: "gone", FlowName: "Gone", Variables: map[string]any{"x": 1.0}})
	dst, _ := emptyDestination()
	dst.Flows = append(dst.Flows, FlowStore{ID: "n3", Name: "Heating", Vars: vars.NewStore()}) // "Heating" now ambiguous

	rep, err := Restore(b, "", dst, Options{})
	require.NoError(t, err)
	assert.Equal(t, []FlowRef{{ID: "f1", Name: "Heating"}, {ID: "gone", Name: "Gone"}}, rep.UnmatchedFlows)
	assert.Equal(t, Counts{}, rep.FlowVariables)
}

// A file that is not an export of this format, or has invalid names, is
// refused before anything is written.
func TestRestoreRefusesInvalidBundles(t *testing.T) {
	dst, _ := emptyDestination()
	_, err := Restore(&Bundle{Format: "other", Version: 1}, "", dst, Options{})
	assert.ErrorIs(t, err, ErrInvalidBundle)
	_, err = Restore(&Bundle{Format: Format, Version: 99}, "", dst, Options{})
	assert.ErrorIs(t, err, ErrInvalidBundle)
	_, err = Restore(&Bundle{Format: Format, Version: Version, Variables: map[string]any{"bad name": 1.0, "ok": 2.0}}, "", dst, Options{})
	assert.ErrorIs(t, err, ErrInvalidBundle)
	_, ok := dst.Globals.Get("ok")
	assert.False(t, ok, "nothing written")
}

// A connection from an export without a passphrase has no password: a new
// one is created without it (and reported), an existing one keeps its own.
func TestRestoreWithoutPasswordsKeepsTheExistingOnes(t *testing.T) {
	b, err := Export(fixture(t), "")
	require.NoError(t, err)
	dst, _ := emptyDestination()
	require.NoError(t, dst.Connections.Set(brokers.Connection{Name: "home", Broker: "tcp://old:1883", Password: "kept-password"}))

	rep, err := Restore(b, "", dst, Options{Overwrite: true})
	require.NoError(t, err)
	home, _ := dst.Connections.Get("home")
	assert.Equal(t, "tcp://10.0.0.2:1883", home.Broker, "replaced")
	assert.Equal(t, "kept-password", home.Password, "its own password kept")
	lab, _ := dst.Connections.Get("lab")
	assert.Empty(t, lab.Password)
	assert.Equal(t, []string{"lab"}, rep.PasswordsNotRestored)
	assert.Equal(t, Counts{Added: 1, Replaced: 1}, rep.Connections)
}

// Importing a connection identical to the one here keeps it (skipped) and
// does not list it as changed, so no flow restarts for nothing.
func TestRestoreSkipsIdenticalConnections(t *testing.T) {
	b, err := Export(fixture(t), passphrase)
	require.NoError(t, err)
	dst := fixture(t)
	rep, err := Restore(b, passphrase, dst, Options{Overwrite: true})
	require.NoError(t, err)
	assert.Equal(t, Counts{Skipped: 2}, rep.Connections)
	assert.Equal(t, Counts{Skipped: 3}, rep.LLMConnections)
	assert.Empty(t, rep.ChangedConnections)
}

// A file asking for more scrypt memory than PiWorker writes is refused
// before deriving anything.
func TestOpenRefusesHeavyScryptParameters(t *testing.T) {
	b, err := Export(fixture(t), passphrase)
	require.NoError(t, err)
	heavy := *b.Sealed
	heavy.N = 1 << 20
	_, err = heavy.Open(passphrase)
	assert.ErrorIs(t, err, ErrInvalidBundle)
}

// Without a passphrase the LLM connections travel without their API keys
// (not even a {{secret.NAME}} reference), each saying whether it had one.
func TestExportCarriesLLMConnectionsWithoutTheirKeys(t *testing.T) {
	b, err := Export(fixture(t), "")
	require.NoError(t, err)
	assert.Equal(t, []LLMConnection{
		{Name: "claude", Provider: "anthropic", Model: "claude-sonnet-4-5", DailyTokenLimit: 100000, HasAPIKey: true},
		{Name: "ollama", Provider: "openai", BaseURL: "http://10.0.0.5:11434/v1", Model: "llama3.2"},
		{Name: "router", Provider: "openai", BaseURL: "https://openrouter.ai/api/v1", HasAPIKey: true},
	}, b.LLMConnections)
	raw, err := json.Marshal(b)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "zzSECRETzz")
	assert.NotContains(t, string(raw), "{{secret.api_key}}")
	assert.Contains(t, string(raw), `"llmConnections":[`)
}

// With a passphrase the LLM connections travel whole and only sealed, like
// the broker connections: editing the readable part cannot send a key to
// another endpoint.
func TestExportSealsLLMConnections(t *testing.T) {
	b, err := Export(fixture(t), passphrase)
	require.NoError(t, err)
	assert.Empty(t, b.LLMConnections)
	raw, err := json.Marshal(b)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "zzSECRETzz")
	assert.NotContains(t, string(raw), "10.0.0.5", "the sealed connections' endpoints are not readable either")

	content, err := b.Sealed.Open(passphrase)
	require.NoError(t, err)
	require.Len(t, content.LLMConnections, 3)
	assert.Equal(t, LLMConnection{Name: "claude", Provider: "anthropic", APIKey: "zzSECRETzzANTHROPIC", Model: "claude-sonnet-4-5",
		DailyTokenLimit: 100000, HasAPIKey: true}, content.LLMConnections[0])
}

// From a file without keys, an existing connection keeps the key it has
// here; a new OpenAI-compatible one is created without it, and an Anthropic
// one, which cannot work without a key, is not created; both are reported.
func TestRestoreLLMConnectionsWithoutKeys(t *testing.T) {
	b, err := Export(fixture(t), "")
	require.NoError(t, err)

	dst, _ := emptyDestination()
	rep, err := Restore(b, "", dst, Options{})
	require.NoError(t, err)
	_, ok := dst.LLMConnections.Get("claude")
	assert.False(t, ok, "an Anthropic connection needs its key")
	router, ok := dst.LLMConnections.Get("router")
	require.True(t, ok)
	assert.Empty(t, router.APIKey)
	ollama, ok := dst.LLMConnections.Get("ollama")
	require.True(t, ok)
	assert.Equal(t, "llama3.2", ollama.Model)
	assert.Equal(t, Counts{Added: 2, Skipped: 1}, rep.LLMConnections)
	assert.Equal(t, []string{"claude", "router"}, rep.APIKeysNotRestored)

	dst, _ = emptyDestination()
	require.NoError(t, dst.LLMConnections.Set(llm.Connection{Name: "claude", Provider: llm.ProviderAnthropic, APIKey: "kept-key", Model: "old"}))
	rep, err = Restore(b, "", dst, Options{Overwrite: true})
	require.NoError(t, err)
	claude, _ := dst.LLMConnections.Get("claude")
	assert.Equal(t, "claude-sonnet-4-5", claude.Model, "replaced")
	assert.Equal(t, "kept-key", claude.APIKey, "its own key kept")
	assert.Equal(t, int64(100000), claude.DailyTokenLimit)
	assert.Equal(t, Counts{Added: 2, Replaced: 1}, rep.LLMConnections)
	assert.Equal(t, []string{"router"}, rep.APIKeysNotRestored)
}

// LLM connections are checked as the store would before anything is
// written, in the readable and in the sealed part; the error never quotes
// a key.
func TestRestoreRefusesInvalidLLMConnections(t *testing.T) {
	valid, err := Export(fixture(t), "")
	require.NoError(t, err)
	for name, bad := range map[string]LLMConnection{
		"provider":   {Name: "x", Provider: "gemini"},
		"name":       {Name: "bad name", Provider: "openai"},
		"base URL":   {Name: "x", Provider: "openai", BaseURL: "ftp://example.com"},
		"credential": {Name: "x", Provider: "openai", BaseURL: "https://user:pw@example.com/v1"},
		"limit":      {Name: "x", Provider: "openai", DailyTokenLimit: -1},
		"no key":     {Name: "x", Provider: "anthropic"},
	} {
		t.Run(name, func(t *testing.T) {
			b := *valid
			b.LLMConnections = append(append([]LLMConnection{}, valid.LLMConnections...), bad)
			dst, _ := emptyDestination()
			_, err := Restore(&b, "", dst, Options{})
			require.ErrorIs(t, err, ErrInvalidBundle)
			_, ok := dst.Secrets.Get("api_key")
			assert.False(t, ok)
			assert.Empty(t, dst.LLMConnections.Names(), "nothing written")
			v, _ := dst.Globals.Get("threshold")
			assert.Nil(t, v, "nothing written")
		})
	}

	sealed, err := seal(SealedContent{Secrets: map[string]string{}, LLMConnections: []LLMConnection{
		{Name: "evil", Provider: "openai", BaseURL: "https://zzSECRETzzKEY@example.com", APIKey: "zzSECRETzzKEY", HasAPIKey: true},
	}}, passphrase)
	require.NoError(t, err)
	b := &Bundle{Format: Format, Version: Version, Sealed: sealed}
	dst, _ := emptyDestination()
	_, err = Restore(b, passphrase, dst, Options{})
	require.ErrorIs(t, err, ErrInvalidBundle)
	assert.NotContains(t, err.Error(), "zzSECRETzz")
	assert.Empty(t, dst.LLMConnections.Names())
}

// A destination without an LLM connection store (older wiring) restores
// everything else and counts the file's LLM connections as skipped; an
// export without one carries none.
func TestRestoreWithoutAnLLMStoreSkipsLLMConnections(t *testing.T) {
	b, err := Export(fixture(t), passphrase)
	require.NoError(t, err)
	dst, _ := emptyDestination()
	dst.LLMConnections = nil
	rep, err := Restore(b, passphrase, dst, Options{})
	require.NoError(t, err)
	assert.Equal(t, Counts{Skipped: 3}, rep.LLMConnections)
	assert.Equal(t, Counts{Added: 2}, rep.Secrets)

	src := fixture(t)
	src.LLMConnections = nil
	b, err = Export(src, "")
	require.NoError(t, err)
	assert.Equal(t, []LLMConnection{}, b.LLMConnections)
}

// A file written before LLM connections existed (no llmConnections member)
// still restores.
func TestRestoreFileWithoutLLMConnections(t *testing.T) {
	b, err := Export(fixture(t), "")
	require.NoError(t, err)
	raw, err := json.Marshal(b)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	delete(m, "llmConnections")
	raw, err = json.Marshal(m)
	require.NoError(t, err)
	var old Bundle
	require.NoError(t, json.Unmarshal(raw, &old))
	dst, _ := emptyDestination()
	rep, err := Restore(&old, "", dst, Options{})
	require.NoError(t, err)
	assert.Equal(t, Counts{}, rep.LLMConnections)
	assert.Equal(t, []string{}, rep.APIKeysNotRestored)
}

// From a file without keys, an existing connection keeps its key only while
// the file leaves it at the same endpoint: the readable part is not
// authenticated, so editing it must not send this installation's key
// somewhere else. Such a connection is restored without the key (an
// Anthropic one, which needs a key, is left as it is) and reported.
func TestRestoreDoesNotSendAKeptKeyToAnotherEndpoint(t *testing.T) {
	b, err := Export(fixture(t), "")
	require.NoError(t, err)
	for i := range b.LLMConnections {
		switch b.LLMConnections[i].Name {
		case "claude":
			b.LLMConnections[i].BaseURL = "https://attacker.example.com"
		case "router":
			b.LLMConnections[i].BaseURL = "https://attacker.example.com/v1"
		}
	}
	dst, _ := emptyDestination()
	mine := llm.Connection{Name: "claude", Provider: llm.ProviderAnthropic, APIKey: "kept-key"}
	require.NoError(t, dst.LLMConnections.Set(mine))
	require.NoError(t, dst.LLMConnections.Set(llm.Connection{Name: "router", Provider: llm.ProviderOpenAI, BaseURL: "https://openrouter.ai/api/v1/", APIKey: "router-key"}))

	rep, err := Restore(b, "", dst, Options{Overwrite: true})
	require.NoError(t, err)
	claude, _ := dst.LLMConnections.Get("claude")
	assert.Equal(t, mine, claude, "left as it is")
	router, _ := dst.LLMConnections.Get("router")
	assert.Equal(t, "https://attacker.example.com/v1", router.BaseURL)
	assert.Empty(t, router.APIKey, "the key does not follow the endpoint")
	assert.Equal(t, []string{"claude", "router"}, rep.APIKeysNotRestored)
	assert.Equal(t, Counts{Added: 1, Replaced: 1, Skipped: 1}, rep.LLMConnections)

	// The same endpoint (a trailing slash aside) keeps the key.
	b, err = Export(fixture(t), "")
	require.NoError(t, err)
	dst, _ = emptyDestination()
	require.NoError(t, dst.LLMConnections.Set(llm.Connection{Name: "router", Provider: llm.ProviderOpenAI, BaseURL: "https://openrouter.ai/api/v1/", APIKey: "router-key"}))
	_, err = Restore(b, "", dst, Options{Overwrite: true})
	require.NoError(t, err)
	router, _ = dst.LLMConnections.Get("router")
	assert.Equal(t, "router-key", router.APIKey)
}
