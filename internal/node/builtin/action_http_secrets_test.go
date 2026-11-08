package builtin

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seenRequest is what the test server observed.
type seenRequest struct {
	header   http.Header
	rawQuery string
	path     string
	body     string
	user     string
	pass     string
	hasAuth  bool
}

// setSecret stores a secret for the duration of the test.
func setSecret(t *testing.T, name, value string) {
	t.Helper()
	require.NoError(t, secrets.DefaultStore.Set(name, value))
	t.Cleanup(func() { _ = secrets.DefaultStore.Delete(name) })
}

// secretServer records requests; hits counts them.
func secretServer(t *testing.T) (*httptest.Server, *seenRequest, *atomic.Int32) {
	t.Helper()
	var seen seenRequest
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		seen = seenRequest{header: r.Header.Clone(), rawQuery: r.URL.RawQuery, path: r.URL.Path, body: string(body)}
		seen.user, seen.pass, seen.hasAuth = r.BasicAuth()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, &seen, &hits
}

func runHTTP(t *testing.T, cfg map[string]interface{}, payload interface{}) ([]*types.Message, error) {
	t.Helper()
	cfg["allowPrivate"] = true
	n, err := NewHTTPAction(cfg)
	require.NoError(t, err)
	return n.Process(context.Background(), types.NewMessage(payload, types.DataTypeObject))
}

func TestHTTPHeadersResolveSecrets(t *testing.T) {
	setSecret(t, "api_key", "s3cr3t")
	srv, seen, _ := secretServer(t)

	out, err := runHTTP(t, map[string]interface{}{
		"url":     srv.URL + "/x",
		"headers": map[string]interface{}{"Authorization": "Bearer {{secret.api_key}}", "X-Topic": "{{topic}}"},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, "Bearer s3cr3t", seen.header.Get("Authorization"))
	require.Len(t, out, 1)
	assert.NotContains(t, out[0].Meta["httpURL"], "s3cr3t")
}

// Secrets are resolved only from the configured template, never from text
// that came from the message: otherwise any payload could exfiltrate them.
func TestHTTPPayloadTextNeverResolvesSecrets(t *testing.T) {
	setSecret(t, "api_key", "s3cr3t")
	srv, seen, _ := secretServer(t)

	payload := map[string]interface{}{"v": "{{secret.api_key}}"}
	_, err := runHTTP(t, map[string]interface{}{
		"url":      srv.URL + "/x?q={{payload.v}}",
		"method":   "POST",
		"headers":  map[string]interface{}{"X-Data": "{{payload.v}}"},
		"body":     `{"v":"{{payload.v}}","direct":"{{secret.api_key}}"}`,
		"bodyType": "json",
	}, payload)
	require.NoError(t, err)
	assert.Equal(t, "{{secret.api_key}}", seen.header.Get("X-Data"))
	assert.NotContains(t, seen.rawQuery, "s3cr3t")
	assert.NotContains(t, seen.body, "s3cr3t", "the body never resolves secrets")
}

func TestHTTPWholeURLFromPayloadNeverResolvesSecrets(t *testing.T) {
	setSecret(t, "api_key", "s3cr3t")
	srv, seen, _ := secretServer(t)

	payload := map[string]interface{}{"url": srv.URL + "/x?k={{secret.api_key}}"}
	_, err := runHTTP(t, map[string]interface{}{"url": "{{payload.url}}"}, payload)
	require.NoError(t, err)
	assert.NotContains(t, seen.rawQuery, "s3cr3t")
}

func TestHTTPQueryResolvesSecretsEscaped(t *testing.T) {
	setSecret(t, "api_key", "a&b=c d")
	srv, seen, _ := secretServer(t)

	out, err := runHTTP(t, map[string]interface{}{"url": srv.URL + "/x?key={{secret.api_key}}&n=1"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "key=a%26b%3Dc+d&n=1", seen.rawQuery, "a secret cannot add query parameters")
	require.Len(t, out, 1)
	shown := out[0].Meta["httpURL"].(string)
	assert.NotContains(t, shown, "a&b")
	assert.Contains(t, shown, "{{secret.api_key}}")
}

// Re-serialising the URL after placing a secret keeps payload escaping intact.
func TestHTTPQuerySecretKeepsEscapedPath(t *testing.T) {
	setSecret(t, "api_key", "k")
	var rawPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawPath = r.URL.EscapedPath()
	}))
	t.Cleanup(srv.Close)

	_, err := runHTTP(t, map[string]interface{}{"url": srv.URL + "/items/{{payload.id}}?key={{secret.api_key}}&q={{payload.q}}"},
		map[string]interface{}{"id": "a/b c", "q": "x&y"})
	require.NoError(t, err)
	assert.Equal(t, "/items/a%2Fb%20c", rawPath)
}

func TestHTTPUserinfoResolvesSecrets(t *testing.T) {
	setSecret(t, "svc_user", "bot")
	setSecret(t, "svc_pass", "p@ss:w/rd")
	srv, seen, _ := secretServer(t)

	target := strings.Replace(srv.URL, "http://", "http://{{secret.svc_user}}:{{secret.svc_pass}}@", 1)
	_, err := runHTTP(t, map[string]interface{}{"url": target + "/x"}, nil)
	require.NoError(t, err)
	require.True(t, seen.hasAuth)
	assert.Equal(t, "bot", seen.user)
	assert.Equal(t, "p@ss:w/rd", seen.pass)
	assert.Equal(t, "/x", seen.path)
}

// In the scheme, host, path or fragment a secret would end up in logs,
// metadata and redirects, or choose the destination: refuse before sending.
func TestHTTPSecretsOutsideUserinfoAndQueryAreRejected(t *testing.T) {
	setSecret(t, "api_key", "s3cr3t")
	srv, _, hits := secretServer(t)
	host := strings.TrimPrefix(srv.URL, "http://")

	for _, u := range []string{
		srv.URL + "/{{secret.api_key}}/x",
		srv.URL + "/x#{{secret.api_key}}",
		"http://{{secret.api_key}}.example.com/x",
		"http://" + host + "{{secret.api_key}}/",
	} {
		_, err := runHTTP(t, map[string]interface{}{"url": u}, nil)
		require.Error(t, err, u)
		assert.Contains(t, err.Error(), "{{secret.api_key}}", u)
		assert.NotContains(t, err.Error(), "s3cr3t", u)
	}
	assert.Zero(t, hits.Load(), "nothing may be sent")
}

func TestHTTPMissingSecretFailsBeforeSending(t *testing.T) {
	srv, _, hits := secretServer(t)

	_, err := runHTTP(t, map[string]interface{}{
		"url":     srv.URL + "/x",
		"headers": map[string]interface{}{"Authorization": "Bearer {{secret.nope}}"},
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `secret "nope"`)

	_, err = runHTTP(t, map[string]interface{}{"url": srv.URL + "/x?k={{secret.nope}}"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `secret "nope"`)
	assert.Zero(t, hits.Load())
}

// Secrets are read at request time, so a rotated secret applies to a running
// flow without redeploying it.
func TestHTTPSecretsAreReadAtRequestTime(t *testing.T) {
	setSecret(t, "api_key", "old")
	srv, seen, _ := secretServer(t)

	n, err := NewHTTPAction(map[string]interface{}{
		"url":          srv.URL + "/x?k={{secret.api_key}}",
		"headers":      map[string]interface{}{"Authorization": "{{secret.api_key}}"},
		"allowPrivate": true,
	})
	require.NoError(t, err)
	require.NoError(t, secrets.DefaultStore.Set("api_key", "new"))

	_, err = n.Process(context.Background(), types.NewMessage(nil, types.DataTypeAny))
	require.NoError(t, err)
	assert.Equal(t, "new", seen.header.Get("Authorization"))
	assert.Equal(t, "k=new", seen.rawQuery)
}

func TestHTTPNetworkErrorDoesNotLeakResolvedSecrets(t *testing.T) {
	setSecret(t, "api_key", "s3cr3t-value")
	setSecret(t, "svc_pass", "pa55-value")
	n, err := NewHTTPAction(map[string]interface{}{
		"url": "https://u:{{secret.svc_pass}}@api.example.com/v1?key={{secret.api_key}}",
	})
	require.NoError(t, err)
	a := n.(*HTTPAction)
	a.client = failingClient()

	_, err = a.Process(context.Background(), types.NewMessage(nil, types.DataTypeAny))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "s3cr3t-value")
	assert.NotContains(t, err.Error(), "pa55-value")
}
