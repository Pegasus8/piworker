package service

import (
	"context"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nodeError runs a real node of nodeType with cfg once and returns its
// error (the text the run recorder stores).
func nodeError(t *testing.T, nodeType string, cfg map[string]any) string {
	t.Helper()
	n, err := node.DefaultRegistry.Create(nodeType, cfg)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = n.Process(ctx, types.NewMessage(map[string]any{"x": 1}, types.DataTypeObject))
	require.Error(t, err)
	return err.Error()
}

// pathTokenURL is a webhook URL whose credential sits in the path (as in
// Telegram bot, Discord and Slack webhook URLs), with a query too; nothing
// listens on the address (and action-http refuses loopback targets), so a
// request fails without reaching the network.
const (
	pathToken    = "PATHTOKENxyz1234567890"
	pathTokenURL = "http://127.0.0.1:1/services/T000/B000/" + pathToken + "?wait=true"
)

// A real action-http node that cannot connect records its URL in its error,
// with the path. GetFlow hides that URL (it is not AgentVisible), so GetRun
// must not show it either: the token in the path is a credential.
func TestGetRunHidesTheURLsOfARealNodeError(t *testing.T) {
	e := newReadsEnv(t)
	cfg := map[string]any{"method": "POST", "url": pathTokenURL, "body": "{}"}
	recorded := nodeError(t, "action-http", cfg)
	require.Contains(t, recorded, pathToken, "the node's own error keeps the path (the editor shows it)")

	f := manualFlow("hooks")
	httpN := flow.NewNode("action-http", flow.NodeCategoryOutput)
	httpN.ID = "http"
	httpN.Config = cfg
	f.AddNode(*httpN)
	e.save(f)
	e.addRun(f.ID, "run-1", "", 0, time.Second, "error",
		storage.NodeEventRecord{NodeID: "http", NodeType: "action-http", Phase: "error", Error: recorded})

	ctx := context.Background()
	view, err := e.reads.GetFlow(ctx, readsAgent(storage.ScopeRead), f.ID)
	require.NoError(t, err)
	require.Equal(t, redactedMark, view.Nodes[2].Config["url"], "GetFlow hides the URL")

	run, err := e.reads.GetRun(ctx, readsAgent(storage.ScopeRead), f.ID, "run-1")
	require.NoError(t, err)
	require.Len(t, run.Events, 1)
	shown := run.Events[0].Error
	assert.NotContains(t, leakText(t, run), pathToken, "GetRun leaked the URL's path: %s", shown)
	assert.Contains(t, shown, "http://127.0.0.1:1/"+redactedMark, "the scheme and host are kept for diagnosis")
}

// Recorded and admin-written text shows a URL only up to its host: its
// path, like its userinfo, query and fragment, may hold a credential.
func TestHideURLsInText(t *testing.T) {
	for in, want := range map[string]string{
		"":                                   "",
		"no urls here":                       "no urls here",
		"see https://example.com.":           "see https://example.com.",
		"see https://example.com/ now":       "see https://example.com/ now",
		"https://api.example.com/v1/doors":   "https://api.example.com/<redacted>",
		"(https://api.example.com/v1/doors)": "(https://api.example.com/<redacted>)",
		`Post "https://api.telegram.org/bot123456:AAH-tok/sendMessage?<redacted>": dial tcp: refused`: `Post "https://api.telegram.org/<redacted>": dial tcp: refused`,
		"mqtt://user:pw@broker.local:1883":                       "mqtt://broker.local:1883/<redacted>",
		"https://example.com?key=abc":                            "https://example.com/<redacted>",
		"https://example.com#frag":                               "https://example.com/<redacted>",
		"bad http://u:p%zz@host/x?k=1 end":                       "bad http://<redacted> end",
		"two: http://a.example/x and HTTPS://b.example/y/z?q=1.": "two: http://a.example/<redacted> and https://b.example/<redacted>.",
		"https://discord.com/api/webhooks/123/TOKEN?wait=true":   "https://discord.com/<redacted>",
	} {
		assert.Equal(t, want, hideURLsInText(in), in)
	}
}

// GetRun also redacts, from recorded text, what GetFlow hides of the run's
// flow: the values of settings agents may not see (whole, their longer
// words, and the parts of URLs, templated ones included) wherever an error
// repeats them in another form than a URL. Visible settings, ids and short
// common words are left alone.
func TestGetRunRedactsHiddenSettingsFromRecordedText(t *testing.T) {
	e := newReadsEnv(t)
	f := manualFlow("hidden settings")
	add := func(id, typ string, cfg map[string]any) {
		n := flow.NewNode(typ, flow.NodeCategoryOutput)
		n.ID, n.Config = id, cfg
		f.AddNode(*n)
	}
	add("tg", "action-http", map[string]any{"method": "POST",
		"url": "https://api.telegram.org/bot123456:AAHtelegramTok3n/sendMessage?chat_id={{msg.chat}}"})
	add("hook", "action-http", map[string]any{"method": "POST",
		"url": "https://discord.com/api/webhooks/{{msg.id}}/DiscordTok3nXYZ?wait=true"})
	add("cmd", "action-command", map[string]any{"command": "curl -H 'X-Token: CmdTok3nS3cret' https://example.com/x"})
	add("mq", "action-mqtt", map[string]any{"broker": "tcp://mqttuser:Brk9pw@broker.local:1883", "topic": "home/door",
		"password": "{{secret.MQTT}}"})
	e.save(f)

	errs := []string{
		"telegram: 404 Not Found for /bot123456:AAHtelegramTok3n/sendMessage",
		"discord: 401 for webhook /DiscordTok3nXYZ",
		"exit status 1: curl: (22) X-Token: CmdTok3nS3cret rejected",
		"mqtt: connect mqttuser:Brk9pw@broker.local refused",
		"POST failed after 3 tries: home door timeout",
	}
	var events []storage.NodeEventRecord
	for _, msg := range errs {
		events = append(events, storage.NodeEventRecord{NodeID: "tg", NodeType: "action-http", Phase: "error", Error: msg})
	}
	e.addRun(f.ID, "run-1", "", 0, time.Second, "error", events...)

	run, err := e.reads.GetRun(context.Background(), readsAgent(storage.ScopeRead), f.ID, "run-1")
	require.NoError(t, err)
	out := leakText(t, run)
	for _, hidden := range []string{"AAHtelegramTok3n", "DiscordTok3nXYZ", "CmdTok3nS3cret", "Brk9pw"} {
		assert.NotContains(t, out, hidden)
	}
	require.Len(t, run.Events, len(errs))
	assert.Equal(t, "POST failed after 3 tries: home door timeout", run.Events[4].Error, "visible settings and short words are kept")
	assert.Equal(t, "tg", run.Events[0].NodeID)
}
