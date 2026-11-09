package builtin

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/Pegasus8/piworker/internal/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every builtin secret lookup resolves through the context the node is given
// (secrets.FromContext). A normal runtime passes none, so the process-wide
// store is used; a sandbox (dry-run, test node) passes its own store and must
// never see a value of the real one. Each test below stores a canary in
// secrets.DefaultStore and checks the three cases: no store in the context
// (the canary is used), an empty store (it is not), and a store with another
// value (that value is used: the lookup really goes through the context).

const ctxCanary = "CANARY-real-secret-7f3a9c"

// sandboxCtx returns a context carrying a fresh secret store holding values.
func sandboxCtx(t *testing.T, values map[string]string) context.Context {
	t.Helper()
	store := secrets.NewStore()
	for k, v := range values {
		require.NoError(t, store.Set(k, v))
	}
	return secrets.WithStore(context.Background(), store)
}

func TestExpandSecretsUseContextStore(t *testing.T) {
	setSecret(t, "canary", ctxCanary)

	assert.Equal(t, "x-"+ctxCanary, expandSecrets(context.Background(), "x-{{secret.canary}}"))
	assert.Equal(t, "x-", expandSecrets(sandboxCtx(t, nil), "x-{{secret.canary}}"))
	assert.Equal(t, "x-other", expandSecrets(sandboxCtx(t, map[string]string{"canary": "other"}), "x-{{secret.canary}}"))

	v, err := expandSecretsStrict(context.Background(), "{{secret.canary}}")
	require.NoError(t, err)
	assert.Equal(t, ctxCanary, v)
	_, err = expandSecretsStrict(sandboxCtx(t, nil), "{{secret.canary}}")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), ctxCanary)
	v, err = expandSecretsStrict(sandboxCtx(t, map[string]string{"canary": "other"}), "{{secret.canary}}")
	require.NoError(t, err)
	assert.Equal(t, "other", v)
}

func TestHTTPSecretsResolveThroughContext(t *testing.T) {
	setSecret(t, "canary", ctxCanary)
	srv, seen, hits := secretServer(t)
	n, err := NewHTTPAction(map[string]interface{}{
		"url":          srv.URL + "/x?key={{secret.canary}}",
		"headers":      map[string]interface{}{"Authorization": "Bearer {{secret.canary}}"},
		"allowPrivate": true,
	})
	require.NoError(t, err)
	msg := types.NewMessage(nil, types.DataTypeAny)

	_, err = n.Process(context.Background(), msg)
	require.NoError(t, err)
	assert.Equal(t, "Bearer "+ctxCanary, seen.header.Get("Authorization"))
	assert.Equal(t, "key="+ctxCanary, seen.rawQuery)

	_, err = n.Process(sandboxCtx(t, map[string]string{"canary": "sandbox"}), msg)
	require.NoError(t, err)
	assert.Equal(t, "Bearer sandbox", seen.header.Get("Authorization"))
	assert.Equal(t, "key=sandbox", seen.rawQuery)

	before := hits.Load()
	_, err = n.Process(sandboxCtx(t, nil), msg)
	require.Error(t, err, "a secret missing from the context's store fails the request")
	assert.Equal(t, before, hits.Load(), "nothing is sent")
}

// pathServer records the request paths it receives.
func pathServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), paths...) }
}

func TestNotifySecretsResolveThroughContext(t *testing.T) {
	setSecret(t, "canary", ctxCanary)
	srv, paths := pathServer(t)
	n, err := NewNotifyAction(map[string]interface{}{
		"url": srv.URL + "/hook/{{secret.canary}}", "format": "raw", "allowPrivate": true,
	})
	require.NoError(t, err)
	msg := types.NewMessage("hi", types.DataTypeString)

	_, err = n.Process(context.Background(), msg)
	require.NoError(t, err)
	_, err = n.Process(sandboxCtx(t, map[string]string{"canary": "sandbox"}), msg)
	require.NoError(t, err)
	_, err = n.Process(sandboxCtx(t, nil), msg)
	require.Error(t, err, "a webhook URL whose secret is missing is not posted to")
	assert.NotContains(t, err.Error(), ctxCanary)

	assert.Equal(t, []string{"/hook/" + ctxCanary, "/hook/sandbox"}, paths())
}

// captureTransport records request URLs and answers 200 without any network.
type captureTransport struct {
	mu   sync.Mutex
	urls []string
}

func (c *captureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.urls = append(c.urls, r.URL.String())
	c.mu.Unlock()
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{}")), Request: r}, nil
}

func (c *captureTransport) seen() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.urls...)
}

func TestTelegramSecretsResolveThroughContext(t *testing.T) {
	setSecret(t, "canary", ctxCanary)
	n, err := NewTelegramAction(map[string]interface{}{"botToken": "{{secret.canary}}", "chatId": "42"})
	require.NoError(t, err)
	rt := &captureTransport{}
	n.(*TelegramAction).client = &http.Client{Transport: rt}
	msg := types.NewMessage("hi", types.DataTypeString)

	_, err = n.Process(context.Background(), msg)
	require.NoError(t, err)
	_, err = n.Process(sandboxCtx(t, map[string]string{"canary": "sandbox"}), msg)
	require.NoError(t, err)
	_, err = n.Process(sandboxCtx(t, nil), msg)
	require.Error(t, err, "a bot token whose secret is missing sends nothing")
	assert.NotContains(t, err.Error(), ctxCanary)

	assert.Equal(t, []string{
		"https://api.telegram.org/bot" + ctxCanary + "/sendMessage",
		"https://api.telegram.org/botsandbox/sendMessage",
	}, rt.seen())
}

// authUser decodes the user and password of an "AUTH PLAIN <b64>" line.
func authUser(t *testing.T, line string) string {
	t.Helper()
	fields := strings.Fields(line)
	require.Len(t, fields, 3, line)
	raw, err := base64.StdEncoding.DecodeString(fields[2])
	require.NoError(t, err)
	return strings.TrimPrefix(strings.ReplaceAll(string(raw), "\x00", ":"), ":")
}

func TestEmailSecretsResolveThroughContext(t *testing.T) {
	setSecret(t, "canary", ctxCanary)
	srv := newFakeSMTP(t, nil)
	a := newEmailAction(t, map[string]interface{}{
		"port": srv.port(), "username": "{{secret.canary}}", "password": "pw-{{secret.canary}}",
	})
	msg := types.NewMessage("hi", types.DataTypeString)

	_, err := a.Process(context.Background(), msg)
	require.NoError(t, err)
	_, err = a.Process(sandboxCtx(t, map[string]string{"canary": "sandbox"}), msg)
	require.NoError(t, err)
	_, err = a.Process(sandboxCtx(t, nil), msg)
	require.NoError(t, err, "missing optional credentials mean no AUTH")

	auths := srv.authLines()
	require.Len(t, auths, 2)
	assert.Equal(t, ctxCanary+":pw-"+ctxCanary, authUser(t, auths[0]))
	assert.Equal(t, "sandbox:pw-sandbox", authUser(t, auths[1]))
}

func TestMqttActionSecretsResolveThroughContext(t *testing.T) {
	setSecret(t, "canary", ctxCanary)
	b := newFakeBroker(t)
	for _, tc := range []struct {
		name string
		ctx  context.Context
		want string
	}{
		{"default store", context.Background(), ctxCanary},
		{"sandbox store", sandboxCtx(t, map[string]string{"canary": "sandbox"}), "sandbox"},
		{"empty store", sandboxCtx(t, nil), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newMqttAction(t, map[string]interface{}{
				"broker": b.url(), "topic": "t", "username": "u-{{secret.canary}}", "password": "{{secret.canary}}",
			})
			defer func() { _ = a.Close() }()
			_, err := a.Process(tc.ctx, types.NewMessage("x", types.DataTypeString))
			require.NoError(t, err)
			r := a.client.OptionsReader()
			assert.Equal(t, "u-"+tc.want, r.Username())
			assert.Equal(t, tc.want, r.Password())
		})
	}
}

func TestMqttTriggerSecretsResolveThroughContext(t *testing.T) {
	setSecret(t, "canary", ctxCanary)
	b := newFakeBroker(t)
	for _, tc := range []struct {
		name string
		ctx  context.Context
		want string
	}{
		{"default store", context.Background(), ctxCanary},
		{"sandbox store", sandboxCtx(t, map[string]string{"canary": "sandbox"}), "sandbox"},
		{"empty store", sandboxCtx(t, nil), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := newMqttTrigger(t, map[string]interface{}{
				"broker": b.url(), "topic": "t", "username": "u-{{secret.canary}}", "password": "{{secret.canary}}",
			})
			require.NoError(t, tr.Start(tc.ctx, make(chan *types.Message, 1)))
			defer func() { _ = tr.Stop() }()
			tr.mu.Lock()
			r := tr.client.OptionsReader()
			tr.mu.Unlock()
			assert.Equal(t, "u-"+tc.want, r.Username())
			assert.Equal(t, tc.want, r.Password())
		})
	}
}

func TestDatabaseDSNResolvesThroughContext(t *testing.T) {
	dir := t.TempDir()
	real, sandbox := filepath.Join(dir, "canary.db"), filepath.Join(dir, "sandbox.db")
	setSecret(t, "dbpath", real)
	newDB := func() *DatabaseAction {
		n, err := NewDatabaseAction(map[string]interface{}{"driver": "sqlite", "dsn": "{{secret.dbpath}}", "query": "SELECT 1 AS one"})
		require.NoError(t, err)
		a := n.(*DatabaseAction)
		t.Cleanup(func() { _ = a.Close() })
		return a
	}
	msg := types.NewMessage(nil, types.DataTypeAny)

	_, err := newDB().Process(sandboxCtx(t, nil), msg)
	require.Error(t, err, "a DSN whose secret is missing opens nothing")
	assert.NotContains(t, err.Error(), real)

	_, err = newDB().Process(sandboxCtx(t, map[string]string{"dbpath": sandbox}), msg)
	require.NoError(t, err)
	_, err = os.Stat(real)
	assert.True(t, os.IsNotExist(err), "the sandbox never opens the real database")

	_, err = newDB().Process(context.Background(), msg)
	require.NoError(t, err)
	_, err = os.Stat(real)
	assert.NoError(t, err, "the normal runtime opens the database named by the real secret")
}

func TestWebhookTokenResolvesThroughStartContext(t *testing.T) {
	setSecret(t, "hook_ctx", ctxCanary)
	newHook := func(path string) *WebhookTrigger {
		n, err := NewWebhookTrigger(map[string]interface{}{"path": path, "token": "{{secret.hook_ctx}}"})
		require.NoError(t, err)
		return n.(*WebhookTrigger)
	}

	err := newHook("ctx-hook-empty").Start(sandboxCtx(t, nil), make(chan *types.Message, 1))
	require.Error(t, err, "the token's secret is missing from the context's store")
	assert.False(t, webhook.DefaultHub.Has("ctx-hook-empty"))

	// Requests are checked against the store of the context Start was given.
	sb := newHook("ctx-hook-sandbox")
	require.NoError(t, sb.Start(sandboxCtx(t, map[string]string{"hook_ctx": "sandbox"}), make(chan *types.Message, 1)))
	defer func() { _ = sb.Stop() }()
	assert.Equal(t, webhook.Unauthorized, dispatchToken("ctx-hook-sandbox", ctxCanary))
	assert.Equal(t, webhook.Accepted, dispatchToken("ctx-hook-sandbox", "sandbox"))

	real := newHook("ctx-hook-real")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, real.Start(ctx, make(chan *types.Message, 1)))
	defer func() { _ = real.Stop() }()
	assert.Equal(t, webhook.Accepted, dispatchToken("ctx-hook-real", ctxCanary))
}

// TestNoBuiltinFactoryReadsSecrets: constructing a node never resolves a
// secret, so an instance built for a sandbox (or for validation) holds no
// secret value. Secrets are read when the node runs, through its context.
func TestNoBuiltinFactoryReadsSecrets(t *testing.T) {
	setSecret(t, "canary", ctxCanary)
	ref := "{{secret.canary}}"
	resolved := ctxCanary
	require.Contains(t, dumpFields(&struct{ s *string }{&resolved}), ctxCanary, "the walker sees unexported fields")
	for typ, cfg := range map[string]map[string]interface{}{
		"action-http":     {"url": "https://example.com/?k=" + ref, "headers": map[string]interface{}{"Authorization": ref}},
		"action-notify":   {"url": "https://example.com/hook/" + ref},
		"action-telegram": {"botToken": ref, "chatId": "1"},
		"action-email":    {"host": "smtp.example.com", "from": "a@example.com", "to": "b@example.com", "username": ref, "password": ref},
		"action-mqtt":     {"broker": "tcp://127.0.0.1:1", "topic": "t", "username": ref, "password": ref},
		"trigger-mqtt":    {"broker": "tcp://127.0.0.1:1", "topic": "t", "username": ref, "password": ref},
		"action-database": {"driver": "sqlite", "dsn": ref, "query": "SELECT 1"},
		"trigger-webhook": {"path": "factory-hook", "token": ref},
	} {
		t.Run(typ, func(t *testing.T) {
			n, err := node.DefaultRegistry.Create(typ, cfg)
			require.NoError(t, err)
			require.NoError(t, n.Validate())
			assert.NotContains(t, dumpFields(n), ctxCanary, "the factory resolved a secret")
		})
	}
}

// dumpFields returns every string reachable from a node's fields (through
// pointers, structs, slices and maps, a few levels deep).
func dumpFields(n interface{}) string {
	var out []string
	seen := map[uintptr]bool{}
	var walk func(v reflect.Value, depth int)
	walk = func(v reflect.Value, depth int) {
		if depth > 6 || !v.IsValid() {
			return
		}
		switch v.Kind() {
		case reflect.String:
			out = append(out, v.String())
		case reflect.Pointer, reflect.Interface:
			if v.IsNil() {
				return
			}
			if v.Kind() == reflect.Pointer {
				if seen[v.Pointer()] {
					return
				}
				seen[v.Pointer()] = true
			}
			walk(v.Elem(), depth+1)
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				walk(v.Field(i), depth+1)
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i), depth+1)
			}
		case reflect.Map:
			iter := v.MapRange()
			for iter.Next() {
				walk(iter.Key(), depth+1)
				walk(iter.Value(), depth+1)
			}
		}
	}
	walk(reflect.ValueOf(n), 0)
	return strings.Join(out, "\n")
}
