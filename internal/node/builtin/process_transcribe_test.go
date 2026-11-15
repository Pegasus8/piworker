package builtin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fake audio: the first bytes are what http.DetectContentType recognizes.
var (
	fakeMP3 = append([]byte("ID3\x03\x00\x00\x00\x00\x00\x00"), []byte("mp3 frames")...)
	fakeOgg = append([]byte("OggS\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00"), []byte("opus packets")...)
)

// fakeTranscription is one request a fakeTranscriber received.
type fakeTranscription struct {
	path     string
	fields   map[string]string
	filename string
	fileType string
	file     []byte
}

// fakeTranscriber is an OpenAI-compatible transcription endpoint (multipart
// requests). Its fakeLLM gives the test environment (env).
type fakeTranscriber struct {
	*fakeLLM

	mu     sync.Mutex
	calls  []fakeTranscription
	status int
	answer any
}

func newFakeTranscriber(t *testing.T, text string) *fakeTranscriber {
	t.Helper()
	ft := &fakeTranscriber{fakeLLM: &fakeLLM{}, status: http.StatusOK, answer: map[string]any{"text": text}}
	ft.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := fakeTranscription{path: r.URL.Path, fields: map[string]string{}}
		if err := r.ParseMultipartForm(64 << 20); err == nil {
			for k, v := range r.MultipartForm.Value {
				call.fields[k] = v[0]
			}
			if files := r.MultipartForm.File["file"]; len(files) == 1 {
				call.filename = files[0].Filename
				call.fileType = files[0].Header.Get("Content-Type")
				if f, err := files[0].Open(); err == nil {
					call.file, _ = io.ReadAll(f)
					_ = f.Close()
				}
			}
		}
		ft.mu.Lock()
		ft.calls = append(ft.calls, call)
		status, answer := ft.status, ft.answer
		ft.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(answer)
	}))
	t.Cleanup(ft.srv.Close)
	return ft
}

func (ft *fakeTranscriber) Calls() []fakeTranscription {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	return append([]fakeTranscription(nil), ft.calls...)
}

func mustTranscribe(t *testing.T, cfg map[string]any) *TranscribeProcess {
	t.Helper()
	n, err := NewTranscribeProcess(cfg)
	require.NoError(t, err)
	require.NoError(t, n.Validate())
	return n.(*TranscribeProcess)
}

func TestTranscribeInfo(t *testing.T) {
	info := TranscribeProcessInfo()
	assert.Equal(t, "process-transcribe", info.Type)
	assert.Equal(t, types.NodeCategoryProcessing, info.Category)
	assert.Equal(t, node.RiskExternal, info.Risk)
	assert.Equal(t, "audio-lines", info.Icon)
	require.NotNil(t, info.Config)
	assert.ElementsMatch(t, []string{"connection", "audio"}, info.Config.Required)
	props := info.Config.Properties
	assert.Equal(t, node.OptionsLLMConnections, props["connection"].Options)
	assert.True(t, props["connection"].Destination)
	assert.Equal(t, node.OptionsLLMModels, props["model"].Options)
	assert.Equal(t, "payload", props["audio"].Default)
	assert.Equal(t, 120, props["timeout"].Default)
	assert.Equal(t, "seconds", props["timeout"].Unit)
	for _, visible := range []string{"connection", "model", "audio", "language", "resultField", "timeout"} {
		assert.Truef(t, props[visible].AgentVisible, "%s is AgentVisible", visible)
	}
	assert.Contains(t, info.Documentation, "## Configuration")
	in, out := mustTranscribe(t, map[string]any{"connection": "c"}).Ports()
	assert.Equal(t, info.Inputs, in)
	assert.Equal(t, info.Outputs, out)
}

func TestTranscribeRefusesBadSettings(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{"blank audio", map[string]any{"audio": " "}, "audio is required"},
		{"audio not a path", map[string]any{"audio": "voice"}, "audio must be a message path"},
		{"audio a template", map[string]any{"audio": "{{payload.voice}}"}, "audio must be a message path"},
		{"language a name", map[string]any{"language": "english"}, "language must be"},
		{"language not text", map[string]any{"language": 1}, "language must be text"},
		{"resultField", map[string]any{"resultField": "a..b"}, "resultField"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := map[string]any{"connection": "c"}
			for k, v := range tc.cfg {
				cfg[k] = v
			}
			_, err := NewTranscribeProcess(cfg)
			assert.ErrorContains(t, err, tc.want)
		})
	}
	n := mustTranscribe(t, map[string]any{"connection": "c", "language": " ES "})
	assert.Equal(t, "es", n.language, "the language code is trimmed and lower-cased")
	assert.Equal(t, "payload", mustTranscribe(t, map[string]any{"connection": "c"}).audio, "audio defaults to the payload")
}

func TestTranscribeSendsTheAudio(t *testing.T) {
	ft := newFakeTranscriber(t, "turn on the lights")
	env := ft.env(t)
	n := mustTranscribe(t, map[string]any{"connection": fakeLLMConnection, "language": "en"})
	in := types.NewMessage(base64.StdEncoding.EncodeToString(fakeMP3), types.DataTypeString)
	in.SourcePort = "true"
	outs, err := n.Process(env.ctx, in)
	require.NoError(t, err)
	assert.Equal(t, "turn on the lights", llmAnswerText(t, outs))
	assert.Equal(t, "output", outs[0].SourcePort)
	assert.Equal(t, map[string]any{"connection": fakeLLMConnection, "model": fakeLLMModel, "requests": int64(1)}, outs[0].Meta["llm"])

	calls := ft.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "/v1/audio/transcriptions", calls[0].path)
	assert.Equal(t, fakeMP3, calls[0].file)
	assert.Equal(t, "audio.mp3", calls[0].filename)
	assert.Equal(t, "audio/mpeg", calls[0].fileType)
	assert.Equal(t, map[string]string{"model": fakeLLMModel, "language": "en", "response_format": "json"}, calls[0].fields)
	assert.Equal(t, llm.Usage{Requests: 1}, env.store.UsageToday(fakeLLMConnection), "one request, no tokens")
}

// The model is the node's, else the connection's, else whisper-1.
func TestTranscribeModel(t *testing.T) {
	ft := newFakeTranscriber(t, "x")
	audio := base64.StdEncoding.EncodeToString(fakeMP3)

	env := ft.env(t)
	_, err := mustTranscribe(t, map[string]any{"connection": fakeLLMConnection, "model": "my-whisper"}).
		Process(env.ctx, types.NewMessage(audio, types.DataTypeString))
	require.NoError(t, err)

	env = ft.env(t, func(c *llm.Connection) { c.Model = "" })
	outs, err := mustTranscribe(t, map[string]any{"connection": fakeLLMConnection}).
		Process(env.ctx, types.NewMessage(audio, types.DataTypeString))
	require.NoError(t, err)
	assert.Equal(t, "whisper-1", outs[0].Meta["llm"].(map[string]any)["model"])

	calls := ft.Calls()
	require.Len(t, calls, 2)
	assert.Equal(t, "my-whisper", calls[0].fields["model"])
	assert.Equal(t, "whisper-1", calls[1].fields["model"])
	_, sent := calls[1].fields["language"]
	assert.False(t, sent, "no language: the model detects it")
}

func TestTranscribeReadsTheAudioPath(t *testing.T) {
	ft := newFakeTranscriber(t, "hello")
	env := ft.env(t)
	n := mustTranscribe(t, map[string]any{"connection": fakeLLMConnection, "audio": "payload.voice", "resultField": "transcript"})

	t.Run("a data URL", func(t *testing.T) {
		in := types.NewMessage(map[string]any{"voice": "data:audio/ogg;base64," + base64.StdEncoding.EncodeToString(fakeOgg), "chat": 7}, types.DataTypeObject)
		outs, err := n.Process(env.ctx, in)
		require.NoError(t, err)
		require.Len(t, outs, 1)
		assert.Equal(t, "hello", outs[0].Payload.(map[string]any)["transcript"])
		assert.Equal(t, 7, outs[0].Payload.(map[string]any)["chat"], "the rest of the payload stays")
		calls := ft.Calls()
		assert.Equal(t, fakeOgg, calls[len(calls)-1].file)
		assert.Equal(t, "audio/ogg", calls[len(calls)-1].fileType)
	})

	t.Run("bytes", func(t *testing.T) {
		in := types.NewMessage(map[string]any{"voice": fakeMP3}, types.DataTypeObject)
		_, err := n.Process(env.ctx, in)
		require.NoError(t, err)
		calls := ft.Calls()
		assert.Equal(t, fakeMP3, calls[len(calls)-1].file)
	})

	t.Run("a list of one", func(t *testing.T) {
		in := types.NewMessage(map[string]any{"voice": []any{base64.StdEncoding.EncodeToString(fakeMP3)}}, types.DataTypeObject)
		_, err := n.Process(env.ctx, in)
		require.NoError(t, err)
	})

	before := len(ft.Calls())
	for name, payload := range map[string]any{
		"no audio at the path": map[string]any{"text": "hi"},
		"two audios":           map[string]any{"voice": []any{base64.StdEncoding.EncodeToString(fakeMP3), base64.StdEncoding.EncodeToString(fakeOgg)}},
		"an empty list":        map[string]any{"voice": []any{}},
		"an image":             map[string]any{"voice": base64.StdEncoding.EncodeToString(tinyPNG)},
		"text":                 map[string]any{"voice": "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte("not audio"))},
		"not base64":           map[string]any{"voice": "hello there!"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := n.Process(env.ctx, types.NewMessage(payload, types.DataTypeObject))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "audio (payload.voice)")
			assert.False(t, node.IsRetryable(err))
		})
	}
	assert.Len(t, ft.Calls(), before, "nothing was sent for the bad messages")
}

// audioServer serves body at any path with the content type (none when
// empty) and status, and counts the requests.
func audioServer(t *testing.T, status int, contentType string, body []byte) (*httptest.Server, *int) {
	t.Helper()
	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		} else {
			w.Header()["Content-Type"] = nil // no sniffing by net/http
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// allowLoopback lets n fetch from the test's loopback servers (its own
// client refuses them, as it refuses every private address).
func allowLoopback(n *TranscribeProcess) *TranscribeProcess {
	n.fetch = newGuardedHTTPClient(0, true)
	return n
}

func TestTranscribeFetchesAudioURLs(t *testing.T) {
	ft := newFakeTranscriber(t, "from a URL")
	env := ft.env(t)

	for _, tc := range []struct {
		name, contentType, path string
		body                    []byte
		wantType, wantFile      string
	}{
		{"declared type", "audio/mpeg", "/voice", fakeMP3, "audio/mpeg", "audio.mp3"},
		{"declared with parameters", "audio/ogg; codecs=opus", "/voice", fakeOgg, "audio/ogg", "audio.ogg"},
		{"sniffed", "application/octet-stream", "/voice", fakeOgg, "application/ogg", "audio.ogg"},
		{"no type", "", "/voice", fakeMP3, "audio/mpeg", "audio.mp3"},
		{"by extension", "application/octet-stream", "/file/voice.m4a", []byte{0, 1, 2, 3, 'm', '4', 'a'}, "audio/mp4", "audio.m4a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, hits := audioServer(t, http.StatusOK, tc.contentType, tc.body)
			n := allowLoopback(mustTranscribe(t, map[string]any{"connection": fakeLLMConnection}))
			outs, err := n.Process(env.ctx, types.NewMessage(srv.URL+tc.path, types.DataTypeString))
			require.NoError(t, err)
			assert.Equal(t, "from a URL", llmAnswerText(t, outs))
			assert.Equal(t, 1, *hits)
			calls := ft.Calls()
			last := calls[len(calls)-1]
			assert.Equal(t, tc.body, last.file)
			assert.Equal(t, tc.wantType, last.fileType)
			assert.Equal(t, tc.wantFile, last.filename)
		})
	}
}

// The node's own client refuses private and loopback addresses (an audio
// URL comes from message data); the error names neither the URL (a
// Telegram file URL carries the bot token) nor a setting the node lacks.
func TestTranscribeRefusesPrivateAudioURLs(t *testing.T) {
	ft := newFakeTranscriber(t, "x")
	env := ft.env(t)
	srv, hits := audioServer(t, http.StatusOK, "audio/mpeg", fakeMP3)
	n := mustTranscribe(t, map[string]any{"connection": fakeLLMConnection})
	_, err := n.Process(env.ctx, types.NewMessage(srv.URL+"/file/bot123:SECRET-TOKEN/voice.oga", types.DataTypeString))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-public address")
	assert.NotContains(t, err.Error(), "SECRET-TOKEN")
	assert.NotContains(t, err.Error(), "allowPrivate")
	assert.False(t, node.IsRetryable(err), "a private address stays private")
	assert.Zero(t, *hits)
	assert.Empty(t, ft.Calls())
}

func TestTranscribeAudioURLErrors(t *testing.T) {
	ft := newFakeTranscriber(t, "x")
	env := ft.env(t)
	for _, tc := range []struct {
		name        string
		status      int
		contentType string
		body        []byte
		want        string
		retryable   bool
	}{
		{"not found", http.StatusNotFound, "text/plain", []byte("no"), "HTTP 404", false},
		{"overloaded", http.StatusServiceUnavailable, "text/plain", []byte("busy"), "HTTP 503", true},
		{"rate limited", http.StatusTooManyRequests, "text/plain", nil, "HTTP 429", true},
		{"an HTML page", http.StatusOK, "text/html; charset=utf-8", []byte("<html>login</html>"), "not audio (text/html)", false},
		{"an image", http.StatusOK, "image/png", tinyPNG, "not audio (image/png)", false},
		{"empty", http.StatusOK, "audio/mpeg", nil, "no content", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := audioServer(t, tc.status, tc.contentType, tc.body)
			n := allowLoopback(mustTranscribe(t, map[string]any{"connection": fakeLLMConnection}))
			_, err := n.Process(env.ctx, types.NewMessage(srv.URL+"/secret-path?token=SECRET", types.DataTypeString))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.NotContains(t, err.Error(), "SECRET")
			assert.NotContains(t, err.Error(), "secret-path")
			assert.Equal(t, tc.retryable, node.IsRetryable(err))
		})
	}
	assert.Empty(t, ft.Calls())
}

func TestFetchAudioIsBounded(t *testing.T) {
	client := newGuardedHTTPClient(0, true)

	t.Run("by the declared length", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Header().Set("Content-Length", "1000")
			_, _ = w.Write(make([]byte, 1000))
		}))
		t.Cleanup(srv.Close)
		_, err := fetchAudio(context.Background(), client, srv.URL, 100)
		assert.ErrorContains(t, err, "too large")
	})

	t.Run("while reading", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "audio/mpeg")
			for i := 0; i < 10; i++ { // chunked: no length up front
				_, _ = w.Write(make([]byte, 50))
				w.(http.Flusher).Flush()
			}
		}))
		t.Cleanup(srv.Close)
		_, err := fetchAudio(context.Background(), client, srv.URL, 100)
		assert.ErrorContains(t, err, "too large")
		m, err := fetchAudio(context.Background(), client, srv.URL, 500)
		require.NoError(t, err)
		assert.Len(t, m.Data, 500)
	})

	t.Run("not a URL", func(t *testing.T) {
		_, err := fetchAudio(context.Background(), client, "http://[::1", 100)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "::1")
	})
}

// Anthropic has no transcription API: the node says so before it fetches
// or sends anything.
func TestTranscribeRefusesAnthropic(t *testing.T) {
	ft := newFakeTranscriber(t, "x")
	env := ft.env(t, func(c *llm.Connection) { c.Provider = llm.ProviderAnthropic; c.APIKey = "sk-ant-test" })
	srv, hits := audioServer(t, http.StatusOK, "audio/mpeg", fakeMP3)
	n := allowLoopback(mustTranscribe(t, map[string]any{"connection": fakeLLMConnection}))
	_, err := n.Process(env.ctx, types.NewMessage(srv.URL, types.DataTypeString))
	require.ErrorIs(t, err, llm.ErrUnsupported)
	assert.Contains(t, err.Error(), "Anthropic")
	assert.Contains(t, err.Error(), "transcription")
	assert.False(t, node.IsRetryable(err))
	assert.Zero(t, *hits)
	assert.Empty(t, ft.Calls())
}

// ---------------------------------------------------------------------------
// What process-transcribe, action-knowledge-add and process-knowledge-search
// share with the other LLM nodes (llm_common_test.go covers process-llm,
// the classifier and the agent with chat answers).

// mediaKnowledgeNode is one of the three nodes with a minimal valid config,
// a message it would send to the provider and the setup that makes it do
// so (the search needs documents).
type mediaKnowledgeNode struct {
	factory func(map[string]any) (node.Node, error)
	config  map[string]any
	payload any
	setup   func(t *testing.T, env llmTestEnv)
}

func mediaKnowledgeNodes() map[string]mediaKnowledgeNode {
	return map[string]mediaKnowledgeNode{
		"process-transcribe": {NewTranscribeProcess, map[string]any{"connection": fakeLLMConnection},
			base64.StdEncoding.EncodeToString(fakeMP3), nil},
		"action-knowledge-add": {NewKnowledgeAddAction, map[string]any{"connection": fakeLLMConnection, "model": "embed", "collection": "docs"},
			"some text", nil},
		"process-knowledge-search": {NewKnowledgeSearchProcess, map[string]any{"connection": fakeLLMConnection, "collection": "docs"},
			"a question", func(t *testing.T, env llmTestEnv) {
				require.NoError(t, env.knowledge.Add("docs", llm.Document{ID: "a", Text: "an answer", Model: "embed", Embedding: llm.Embedding{1, 0}}))
			}},
	}
}

func (m mediaKnowledgeNode) build(t *testing.T, extra map[string]any) node.Node {
	t.Helper()
	cfg := map[string]any{}
	for k, v := range m.config {
		cfg[k] = v
	}
	for k, v := range extra {
		cfg[k] = v
	}
	n, err := m.factory(cfg)
	require.NoError(t, err)
	return n
}

func (m mediaKnowledgeNode) message() *types.Message {
	return types.NewMessage(m.payload, types.DataTypeString)
}

func TestMediaKnowledgeNodesRequireTheirConnection(t *testing.T) {
	for typ, tc := range mediaKnowledgeNodes() {
		t.Run(typ, func(t *testing.T) {
			req, ok := tc.build(t, nil).(node.LLMConnectionRequirer)
			require.True(t, ok, "%s implements node.LLMConnectionRequirer", typ)
			assert.Equal(t, map[string]string{"connection": fakeLLMConnection}, req.RequiredLLMConnections())

			cfg := map[string]any{}
			for k, v := range tc.config {
				cfg[k] = v
			}
			delete(cfg, "connection")
			_, err := tc.factory(cfg)
			assert.ErrorContains(t, err, "connection is required")
			cfg["connection"] = "not a name!"
			_, err = tc.factory(cfg)
			assert.ErrorContains(t, err, "connection")
			cfg["connection"] = fakeLLMConnection
			cfg["timeout"] = 601
			_, err = tc.factory(cfg)
			assert.ErrorContains(t, err, "timeout must be between 1 and 600 seconds")
			cfg["timeout"] = 30
			cfg["model"] = "a\nb"
			_, err = tc.factory(cfg)
			assert.ErrorContains(t, err, "model")
		})
	}
}

func TestMediaKnowledgeNodesRefuseToRunInASandbox(t *testing.T) {
	f := newFakeLLM(t, func(int, string, map[string]any) (int, any) { return 500, apiError("unexpected") })
	for typ, tc := range mediaKnowledgeNodes() {
		t.Run(typ, func(t *testing.T) {
			env := f.env(t)
			if tc.setup != nil {
				tc.setup(t, env)
			}
			sandboxed := node.WithSandbox(env.ctx, node.NewSandbox(1<<20, 1<<20))
			_, err := tc.build(t, nil).Process(sandboxed, tc.message())
			assert.ErrorIs(t, err, llm.ErrSandboxed)
		})
	}
	assert.Empty(t, f.Requests(), "nothing was sent")
}

func TestMediaKnowledgeNodesFailOnAnUndefinedConnection(t *testing.T) {
	ctx := llm.WithStore(context.Background(), llm.NewStore())
	for typ, tc := range mediaKnowledgeNodes() {
		t.Run(typ, func(t *testing.T) {
			_, err := tc.build(t, nil).Process(ctx, tc.message())
			assert.ErrorIs(t, err, llm.ErrUndefined)
			assert.False(t, node.IsRetryable(err))
		})
	}
}

func TestMediaKnowledgeNodesKeepTheErrorClassification(t *testing.T) {
	status := 503
	f := newFakeLLM(t, func(int, string, map[string]any) (int, any) { return status, apiError("overloaded") })
	for typ, tc := range mediaKnowledgeNodes() {
		t.Run(typ, func(t *testing.T) {
			env := f.env(t)
			if tc.setup != nil {
				tc.setup(t, env)
			}
			n := tc.build(t, nil)

			status = 503
			_, err := n.Process(env.ctx, tc.message())
			require.Error(t, err)
			assert.True(t, node.IsRetryable(err), "503 is transient: %v", err)

			status = 400
			_, err = n.Process(env.ctx, tc.message())
			var apiErr *llm.APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, 400, apiErr.Status)
			assert.False(t, node.IsRetryable(err))
		})
	}
}

func TestMediaKnowledgeNodesStopAtTheBudget(t *testing.T) {
	f := newFakeLLM(t, func(int, string, map[string]any) (int, any) { return 500, apiError("unexpected") })
	for typ, tc := range mediaKnowledgeNodes() {
		t.Run(typ, func(t *testing.T) {
			env := f.env(t, func(c *llm.Connection) { c.DailyTokenLimit = 10 })
			require.NoError(t, env.store.RecordUsage(fakeLLMConnection, llm.Usage{InputTokens: 10, Requests: 1}))
			if tc.setup != nil {
				tc.setup(t, env)
			}
			_, err := tc.build(t, nil).Process(env.ctx, tc.message())
			assert.ErrorIs(t, err, llm.ErrBudgetExhausted)
			assert.False(t, node.IsRetryable(err))
		})
	}
	assert.Empty(t, f.Requests())
}

// The timeout bounds the whole call, the fetch of an audio URL included.
func TestMediaKnowledgeNodesTimeOut(t *testing.T) {
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(release) })
	f := &fakeLLM{srv: slow}

	for typ, tc := range mediaKnowledgeNodes() {
		t.Run(typ, func(t *testing.T) {
			env := f.env(t)
			if tc.setup != nil {
				tc.setup(t, env)
			}
			_, err := tc.build(t, map[string]any{"timeout": 1}).Process(env.ctx, tc.message())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "did not finish within 1 second")
			assert.ErrorIs(t, err, context.DeadlineExceeded)
			assert.False(t, node.IsRetryable(err))
		})
	}

	t.Run("fetching the audio", func(t *testing.T) {
		ft := newFakeTranscriber(t, "x")
		env := ft.env(t)
		n := allowLoopback(mustTranscribe(t, map[string]any{"connection": fakeLLMConnection, "timeout": 1}))
		_, err := n.Process(env.ctx, types.NewMessage(slow.URL+"/voice.mp3", types.DataTypeString))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "did not finish within 1 second")
		assert.False(t, node.IsRetryable(err))
		assert.Empty(t, ft.Calls())
	})
}

func TestUnsupportedOnAnthropic(t *testing.T) {
	assert.NoError(t, unsupportedOnAnthropic(llm.Connection{Name: "o", Provider: llm.ProviderOpenAI}, "embeddings", "OpenAI or Ollama"))
	err := unsupportedOnAnthropic(llm.Connection{Name: "claude", Provider: llm.ProviderAnthropic}, "embeddings", "OpenAI or Ollama")
	require.ErrorIs(t, err, llm.ErrUnsupported)
	assert.Equal(t, fmt.Sprintf("embeddings: %s: LLM connection %q is an Anthropic connection, which has no embeddings API; use an OpenAI-compatible connection such as OpenAI or Ollama", llm.ErrUnsupported, "claude"), err.Error())
}

// Formats http.DetectContentType does not know (MP3 without an ID3 tag,
// FLAC, AAC in ADTS frames) sent inline are named by their own signature:
// OpenAI tells the format by the file name's extension, and refuses
// "audio" or "audio.bin".
func TestTranscribeNamesInlineAudioBySignature(t *testing.T) {
	ft := newFakeTranscriber(t, "x")
	env := ft.env(t)
	n := mustTranscribe(t, map[string]any{"connection": fakeLLMConnection})
	for _, tc := range []struct {
		name, file, fileType string
		data                 []byte
	}{
		{"MPEG audio frames", "audio.mp3", "audio/mpeg", append([]byte{0xFF, 0xFB, 0x90, 0x64}, make([]byte, 64)...)},
		{"MPEG-2 layer III frames", "audio.mp3", "audio/mpeg", append([]byte{0xFF, 0xF3, 0x48, 0xC4}, make([]byte, 64)...)},
		{"FLAC", "audio.flac", "audio/flac", append([]byte("fLaC\x00\x00\x00\x22"), make([]byte, 64)...)},
		{"AAC in ADTS frames", "audio.aac", "audio/aac", append([]byte{0xFF, 0xF1, 0x50, 0x80}, make([]byte, 64)...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := n.Process(env.ctx, types.NewMessage(base64.StdEncoding.EncodeToString(tc.data), types.DataTypeString))
			require.NoError(t, err)
			calls := ft.Calls()
			last := calls[len(calls)-1]
			assert.Equal(t, tc.file, last.filename)
			assert.Equal(t, tc.fileType, last.fileType)
			assert.Equal(t, tc.data, last.file)
		})
	}
}
