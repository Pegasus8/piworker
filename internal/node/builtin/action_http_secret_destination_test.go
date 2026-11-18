package builtin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A secret must never go to a destination that message data (a public
// webhook payload, or a variable set from one) can choose: the scheme and
// host have to be written literally whenever the URL or a header uses one.
func TestHTTPSecretsRequireAFixedSchemeAndHost(t *testing.T) {
	bearer := map[string]interface{}{"Authorization": "Bearer {{secret.api_key}}"}
	for _, tc := range []struct {
		name    string
		url     string
		headers map[string]interface{}
	}{
		{"whole URL from payload, secret header", "{{payload.url}}", bearer},
		{"host from payload, secret query", "https://{{payload.host}}/?k={{secret.api_key}}", nil},
		{"base URL from a flow variable", "{{flowVars.base}}/x", bearer},
		{"base URL from a global variable", "{{vars.base}}/x?k={{secret.api_key}}", nil},
		{"scheme from payload", "http{{payload.s}}://api.example.com/?k={{secret.api_key}}", nil},
		{"host suffix from payload", "https://api.example.com{{payload.p}}/", bearer},
		{"port from payload", "https://api.example.com:{{payload.port}}/", bearer},
		{"host label from payload", "https://{{payload.tenant}}.example.com/", bearer},
		{"no scheme", "api.example.com/{{payload.p}}", bearer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := map[string]interface{}{"url": tc.url}
			if tc.headers != nil {
				cfg["headers"] = tc.headers
			}
			_, err := NewHTTPAction(cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "secrets require a fixed scheme and host")
		})
	}
}

func TestHTTPSecretsAllowDynamicPathQueryAndUser(t *testing.T) {
	for _, u := range []string{
		"https://api.example.com/items/{{payload.id}}?k={{secret.api_key}}&q={{payload.q}}",
		"https://{{payload.user}}:{{secret.pass}}@api.example.com/",
		"https://api.example.com:8443/{{payload.p}}#{{payload.f}}",
		"https://api.example.com",
	} {
		_, err := NewHTTPAction(map[string]interface{}{
			"url":     u,
			"headers": map[string]interface{}{"X-Api-Key": "{{secret.api_key}}"},
		})
		assert.NoError(t, err, u)
	}
}

func TestHTTPWithoutSecretsKeepsDynamicDestinations(t *testing.T) {
	for _, u := range []string{"{{payload.url}}", "{{vars.base}}/x", "https://{{payload.host}}/"} {
		_, err := NewHTTPAction(map[string]interface{}{
			"url":     u,
			"headers": map[string]interface{}{"Authorization": "Bearer {{meta.token}}"},
		})
		assert.NoError(t, err, u)
	}
}

func TestDeployRefusesSecretsWithDynamicDestination(t *testing.T) {
	f := flow.NewFlow("exfil")
	trig := flow.NewNode("trigger-manual", flow.NodeCategoryInput)
	act := flow.NewNode("action-http", flow.NodeCategoryOutput)
	act.Config = map[string]interface{}{
		"url":     "{{payload.url}}",
		"headers": map[string]interface{}{"Authorization": "Bearer {{secret.api_key}}"},
	}
	f.AddNode(*trig)
	f.AddNode(*act)
	f.AddConnection(*flow.NewConnection(trig.ID, "output", act.ID, "input"))

	_, err := flow.NewFlowRuntime(f, node.DefaultRegistry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secrets require a fixed scheme and host")
}

// The fixed host may still redirect elsewhere (an open redirect reached
// through a payload-chosen path): headers carrying secrets, and the Referer
// that would carry a query secret, are not forwarded to another host.
func TestHTTPSecretsAreNotForwardedOnCrossHostRedirects(t *testing.T) {
	setSecret(t, "api_key", "s3cr3t")
	var other http.Header
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		other = r.Header.Clone()
	}))
	t.Cleanup(elsewhere.Close)
	var sameHost http.Header
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/same":
			http.Redirect(w, r, "/landing", http.StatusFound)
		case "/landing":
			sameHost = r.Header.Clone()
		default:
			http.Redirect(w, r, elsewhere.URL+"/x", http.StatusFound)
		}
	}))
	t.Cleanup(origin.Close)

	headers := map[string]interface{}{"X-Api-Key": "{{secret.api_key}}", "X-Plain": "visible"}
	_, err := runHTTP(t, map[string]interface{}{"url": origin.URL + "/away?k={{secret.api_key}}", "headers": headers}, nil)
	require.NoError(t, err)
	require.NotNil(t, other)
	assert.Empty(t, other.Get("X-Api-Key"))
	assert.Equal(t, "visible", other.Get("X-Plain"))
	for name, values := range other {
		assert.NotContains(t, strings.Join(values, " "), "s3cr3t", name)
	}

	_, err = runHTTP(t, map[string]interface{}{"url": origin.URL + "/same", "headers": headers}, nil)
	require.NoError(t, err)
	assert.Equal(t, "s3cr3t", sameHost.Get("X-Api-Key"), "same-host redirects keep them")
}
