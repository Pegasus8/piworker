package builtin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPAction(t *testing.T) {
	t.Run("creates with valid config", func(t *testing.T) {
		node, err := NewHTTPAction(map[string]interface{}{
			"url":    "https://example.com/api",
			"method": "POST",
		})
		require.NoError(t, err)
		require.NotNil(t, node)

		action := node.(*HTTPAction)
		assert.Equal(t, "https://example.com/api", action.url)
		assert.Equal(t, "POST", action.method)
	})

	t.Run("fails without URL", func(t *testing.T) {
		_, err := NewHTTPAction(map[string]interface{}{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "url is required")
	})

	t.Run("normalizes method to uppercase", func(t *testing.T) {
		node, err := NewHTTPAction(map[string]interface{}{
			"url":    "https://example.com",
			"method": "post",
		})
		require.NoError(t, err)

		action := node.(*HTTPAction)
		assert.Equal(t, "POST", action.method)
	})

	t.Run("validates HTTP method", func(t *testing.T) {
		_, err := NewHTTPAction(map[string]interface{}{
			"url":    "https://example.com",
			"method": "INVALID",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid HTTP method")
	})

	t.Run("parses headers", func(t *testing.T) {
		node, err := NewHTTPAction(map[string]interface{}{
			"url": "https://example.com",
			"headers": map[string]interface{}{
				"Authorization": "Bearer token123",
				"X-Custom":      "value",
			},
		})
		require.NoError(t, err)

		action := node.(*HTTPAction)
		assert.Equal(t, "Bearer token123", action.headers["Authorization"])
		assert.Equal(t, "value", action.headers["X-Custom"])
	})

	t.Run("enforces timeout limits", func(t *testing.T) {
		// Too low
		node, err := NewHTTPAction(map[string]interface{}{
			"url":     "https://example.com",
			"timeout": 100,
		})
		require.NoError(t, err)
		action := node.(*HTTPAction)
		assert.Equal(t, 1000, int(action.timeout.Milliseconds()))

		// Too high
		node, err = NewHTTPAction(map[string]interface{}{
			"url":     "https://example.com",
			"timeout": 120000,
		})
		require.NoError(t, err)
		action = node.(*HTTPAction)
		assert.Equal(t, 60000, int(action.timeout.Milliseconds()))
	})
}

func TestHTTPActionProcess(t *testing.T) {
	ctx := context.Background()

	t.Run("GET request returns JSON body", func(t *testing.T) {
		// Create test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "GET", r.Method)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"result": "success"})
		}))
		defer server.Close()

		node, err := NewHTTPAction(map[string]interface{}{
			"url":          server.URL,
			"method":       "GET",
			"outputMode":   "body",
			"allowPrivate": true,
		})
		require.NoError(t, err)

		action := node.(*HTTPAction)
		msg := types.NewMessage(nil, types.DataTypeAny)

		outputs, err := action.Process(ctx, msg)
		require.NoError(t, err)
		require.Len(t, outputs, 1)

		payload := outputs[0].Payload.(map[string]interface{})
		assert.Equal(t, "success", payload["result"])
	})

	t.Run("POST request with body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "POST", r.Method)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			assert.Equal(t, "hello", body["message"])

			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"id": "123"})
		}))
		defer server.Close()

		node, err := NewHTTPAction(map[string]interface{}{
			"url":          server.URL,
			"method":       "POST",
			"body":         `{"message": "hello"}`,
			"allowPrivate": true,
		})
		require.NoError(t, err)

		action := node.(*HTTPAction)
		msg := types.NewMessage(nil, types.DataTypeAny)

		outputs, err := action.Process(ctx, msg)
		require.NoError(t, err)
		require.Len(t, outputs, 1)

		// Check metadata
		status, ok := outputs[0].GetMeta("httpStatus")
		assert.True(t, ok)
		assert.Equal(t, 201, status)
	})

	t.Run("full output mode includes all details", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Custom-Header", "custom-value")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data": "test"}`))
		}))
		defer server.Close()

		node, err := NewHTTPAction(map[string]interface{}{
			"url":          server.URL,
			"outputMode":   "full",
			"allowPrivate": true,
		})
		require.NoError(t, err)

		action := node.(*HTTPAction)
		msg := types.NewMessage(nil, types.DataTypeAny)

		outputs, err := action.Process(ctx, msg)
		require.NoError(t, err)
		require.Len(t, outputs, 1)

		payload := outputs[0].Payload.(map[string]interface{})
		assert.Equal(t, 200, payload["status"])
		assert.NotNil(t, payload["headers"])
		assert.NotNil(t, payload["body"])
		assert.NotNil(t, payload["duration"])
	})

	t.Run("status output mode returns only status code", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		}))
		defer server.Close()

		node, err := NewHTTPAction(map[string]interface{}{
			"url":          server.URL,
			"outputMode":   "status",
			"allowPrivate": true,
		})
		require.NoError(t, err)

		action := node.(*HTTPAction)
		msg := types.NewMessage(nil, types.DataTypeAny)

		outputs, err := action.Process(ctx, msg)
		require.NoError(t, err)
		require.Len(t, outputs, 1)

		assert.Equal(t, 202, outputs[0].Payload)
	})
}

func TestHTTPActionTemplateExpansion(t *testing.T) {
	ctx := context.Background()

	t.Run("expands payload in URL", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/users/123", r.URL.Path)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		node, err := NewHTTPAction(map[string]interface{}{
			"url":          server.URL + "/users/{{payload.userId}}",
			"allowPrivate": true,
		})
		require.NoError(t, err)

		action := node.(*HTTPAction)
		msg := types.NewMessage(map[string]interface{}{
			"userId": "123",
		}, types.DataTypeObject)

		_, err = action.Process(ctx, msg)
		require.NoError(t, err)
	})

	t.Run("expands payload in body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			assert.Equal(t, "John", body["name"])
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		node, err := NewHTTPAction(map[string]interface{}{
			"url":          server.URL,
			"method":       "POST",
			"body":         `{"name": "{{payload.name}}"}`,
			"allowPrivate": true,
		})
		require.NoError(t, err)

		action := node.(*HTTPAction)
		msg := types.NewMessage(map[string]interface{}{
			"name": "John",
		}, types.DataTypeObject)

		_, err = action.Process(ctx, msg)
		require.NoError(t, err)
	})

	t.Run("expands topic and id", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.NotEmpty(t, r.Header.Get("X-Message-Id"))
			assert.Equal(t, "test-topic", r.Header.Get("X-Topic"))
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		node, err := NewHTTPAction(map[string]interface{}{
			"url": server.URL,
			"headers": map[string]interface{}{
				"X-Message-Id": "{{id}}",
				"X-Topic":      "{{topic}}",
			},
			"allowPrivate": true,
		})
		require.NoError(t, err)

		action := node.(*HTTPAction)
		msg := types.NewMessage(nil, types.DataTypeAny)
		msg.Topic = "test-topic"

		_, err = action.Process(ctx, msg)
		require.NoError(t, err)
	})

	t.Run("handles nested payload fields", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.String(), "user@example.com")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		node, err := NewHTTPAction(map[string]interface{}{
			"url":          server.URL + "?email={{payload.user.email}}",
			"allowPrivate": true,
		})
		require.NoError(t, err)

		action := node.(*HTTPAction)
		msg := types.NewMessage(map[string]interface{}{
			"user": map[string]interface{}{
				"email": "user@example.com",
			},
		}, types.DataTypeObject)

		_, err = action.Process(ctx, msg)
		require.NoError(t, err)
	})
}

func TestHTTPActionPorts(t *testing.T) {
	node, err := NewHTTPAction(map[string]interface{}{
		"url": "https://example.com",
	})
	require.NoError(t, err)

	inputs, outputs := node.Ports()

	assert.Len(t, inputs, 1)
	assert.Equal(t, "input", inputs[0].ID)
	assert.Len(t, outputs, 1)
	assert.Equal(t, "output", outputs[0].ID)
}

func TestHTTPActionConfigSchema(t *testing.T) {
	node, err := NewHTTPAction(map[string]interface{}{
		"url": "https://example.com",
	})
	require.NoError(t, err)

	action := node.(*HTTPAction)
	schema := action.GetConfigSchema()

	assert.Contains(t, schema.Properties, "url")
	assert.Contains(t, schema.Properties, "method")
	assert.Contains(t, schema.Properties, "headers")
	assert.Contains(t, schema.Properties, "body")
	assert.Contains(t, schema.Properties, "timeout")
	assert.Contains(t, schema.Properties, "outputMode")
	assert.Contains(t, schema.Properties, "allowPrivate")

	assert.Contains(t, schema.Required, "url")
}

func TestHTTPActionSSRFProtection(t *testing.T) {
	ctx := context.Background()

	t.Run("blocks loopback address by default", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		node, err := NewHTTPAction(map[string]interface{}{
			"url":    server.URL, // httptest binds to 127.0.0.1 (loopback)
			"method": "GET",
		})
		require.NoError(t, err)

		_, err = node.(*HTTPAction).Process(ctx, types.NewMessage(nil, types.DataTypeAny))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "non-public address")
	})

	t.Run("allows loopback when allowPrivate is set", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		node, err := NewHTTPAction(map[string]interface{}{
			"url":          server.URL,
			"method":       "GET",
			"allowPrivate": true,
		})
		require.NoError(t, err)

		_, err = node.(*HTTPAction).Process(ctx, types.NewMessage(nil, types.DataTypeAny))
		require.NoError(t, err)
	})

	t.Run("rejects non-http scheme", func(t *testing.T) {
		node, err := NewHTTPAction(map[string]interface{}{
			"url": "file:///etc/passwd",
		})
		require.NoError(t, err)

		_, err = node.(*HTTPAction).Process(ctx, types.NewMessage(nil, types.DataTypeAny))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported URL scheme")
	})
}
