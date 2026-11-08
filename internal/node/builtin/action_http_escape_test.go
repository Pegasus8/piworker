package builtin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type capturedRequest struct {
	path, rawQuery string
	query          url.Values
	body           []byte
}

func captureHTTP(t *testing.T, cfg map[string]interface{}, payload interface{}) capturedRequest {
	t.Helper()
	var got capturedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		got.rawQuery = r.URL.RawQuery
		got.query = r.URL.Query()
		got.body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	full := map[string]interface{}{"allowPrivate": true}
	for k, v := range cfg {
		full[k] = v
	}
	if u, ok := full["url"].(string); ok {
		full["url"] = srv.URL + u
	} else {
		full["url"] = srv.URL
	}
	n, err := NewHTTPAction(full)
	require.NoError(t, err)
	_, err = n.Process(context.Background(), types.NewMessage(payload, types.DataTypeObject))
	require.NoError(t, err)
	return got
}

func TestHTTPActionEscapesQueryValues(t *testing.T) {
	got := captureHTTP(t, map[string]interface{}{"url": "/search?q={{payload.q}}&limit=5"},
		map[string]interface{}{"q": "a&admin=1 b#frag"})
	assert.Equal(t, "a&admin=1 b#frag", got.query.Get("q"))
	assert.Empty(t, got.query.Get("admin"), "value must not inject a query parameter")
	assert.Equal(t, "5", got.query.Get("limit"))
}

func TestHTTPActionEscapesPathValues(t *testing.T) {
	got := captureHTTP(t, map[string]interface{}{"url": "/users/{{payload.id}}/profile"},
		map[string]interface{}{"id": "../admin?x=1"})
	assert.Equal(t, "/users/../admin?x=1/profile", got.path, "value stays one path segment")
	assert.Empty(t, got.rawQuery)
}

func TestHTTPActionWholeURLTemplateIsNotEscaped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/a/b", r.URL.Path)
		assert.Equal(t, "x=1", r.URL.RawQuery)
	}))
	defer srv.Close()
	n, err := NewHTTPAction(map[string]interface{}{"url": "{{payload.url}}", "allowPrivate": true})
	require.NoError(t, err)
	_, err = n.Process(context.Background(),
		types.NewMessage(map[string]interface{}{"url": srv.URL + "/a/b?x=1"}, types.DataTypeObject))
	require.NoError(t, err)
}

func TestHTTPActionEscapesJSONStringValues(t *testing.T) {
	got := captureHTTP(t, map[string]interface{}{
		"method": "POST",
		"body":   `{"name": "{{payload.name}}", "temp": {{payload.temp}}, "raw": {{payload.obj}}}`,
	}, map[string]interface{}{
		"name": "say \"hi\"\n\\ \", \"admin\": true, \"x\": \"",
		"temp": 21.5,
		"obj":  map[string]interface{}{"k": "v"},
	})
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(got.body, &body), "body must stay valid JSON: %s", got.body)
	assert.Equal(t, "say \"hi\"\n\\ \", \"admin\": true, \"x\": \"", body["name"])
	assert.Equal(t, 21.5, body["temp"])
	assert.Equal(t, map[string]interface{}{"k": "v"}, body["raw"])
	assert.NotContains(t, body, "admin")
}

func TestHTTPActionWholeJSONBodyTemplateIsRaw(t *testing.T) {
	got := captureHTTP(t, map[string]interface{}{"method": "POST", "body": "{{payload}}"},
		map[string]interface{}{"a": "b\"c"})
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(got.body, &body))
	assert.Equal(t, "b\"c", body["a"])
}

func TestHTTPActionEscapesFormValues(t *testing.T) {
	got := captureHTTP(t, map[string]interface{}{
		"method": "POST", "bodyType": "form", "body": "user={{payload.u}}&role=viewer",
	}, map[string]interface{}{"u": "bob&role=admin"})
	form, err := url.ParseQuery(string(got.body))
	require.NoError(t, err)
	assert.Equal(t, "bob&role=admin", form.Get("user"))
	assert.Equal(t, []string{"viewer"}, form["role"])
}
