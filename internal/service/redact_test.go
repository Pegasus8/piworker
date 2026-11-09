package service

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const canary = "s3cr3t-Canary/value+42 &x"

func testRedactor(t *testing.T, values ...string) secretRedactor {
	t.Helper()
	store := secrets.NewStore()
	for i, v := range values {
		require.NoError(t, store.Set("s"+string(rune('a'+i)), v))
	}
	return newSecretRedactor(store)
}

func TestRedactTextForms(t *testing.T) {
	r := testRedactor(t, canary)
	forms := map[string]string{
		"literal":               canary,
		"query":                 url.QueryEscape(canary),
		"path":                  url.PathEscape(canary),
		"base64":                base64.StdEncoding.EncodeToString([]byte(canary)),
		"base64url":             base64.URLEncoding.EncodeToString([]byte(canary)),
		"base64raw":             base64.RawStdEncoding.EncodeToString([]byte(canary)),
		"embedded b64":          base64.StdEncoding.EncodeToString([]byte("user:" + canary)),
		"embedded b64 offset 2": base64.StdEncoding.EncodeToString([]byte("ab" + canary + "tail")),
	}
	for name, form := range forms {
		out := r.text("before " + form + " after")
		assert.NotContains(t, out, form, name)
		assert.Contains(t, out, redactedMark, name)
		assert.True(t, strings.HasPrefix(out, "before "), name)
	}
	assert.Equal(t, "nothing to hide", r.text("nothing to hide"))
}

// Very short values would redact ordinary text; they are not secrets worth
// the damage.
func TestRedactIgnoresTinySecrets(t *testing.T) {
	r := testRedactor(t, "ab", "")
	assert.Equal(t, "abc ab", r.text("abc ab"))
}

func TestRedactJSON(t *testing.T) {
	r := testRedactor(t, canary, `quo"te\slash`)
	in, err := json.Marshal(map[string]any{
		"plain":  "ok",
		"leak":   "token=" + canary,
		"nested": []any{map[string]any{"deep": url.QueryEscape(canary)}},
		canary:   1,
		"esc":    `x quo"te\slash y`,
		"num":    json.Number("12345678901234567890"),
	})
	require.NoError(t, err)
	out := r.json(in)
	require.True(t, json.Valid(out), string(out))
	assert.NotContains(t, string(out), "Canary")
	assert.NotContains(t, string(out), `quo\"te`)
	assert.Contains(t, string(out), "12345678901234567890", "numbers are kept exactly")
	assert.Contains(t, string(out), redactedMark)

	// Nothing to hide: the bytes are returned unchanged (key order too).
	clean := json.RawMessage(`{"z":1,"a":"x"}`)
	assert.Equal(t, string(clean), string(r.json(clean)))
}

func TestCapResult(t *testing.T) {
	small := json.RawMessage(`{"ok":true}`)
	assert.Equal(t, string(small), string(capResult(small, 1024)))

	big, err := json.Marshal(map[string]any{"text": strings.Repeat("é\"x", 5000)})
	require.NoError(t, err)
	for _, limit := range []int{1024, 1500, 4096} {
		out := capResult(big, limit)
		assert.LessOrEqual(t, len(out), limit)
		require.True(t, json.Valid(out), string(out))
		var v struct {
			Truncated  bool   `json:"truncated"`
			Preview    string `json:"preview"`
			TotalBytes int    `json:"totalBytes"`
		}
		require.NoError(t, json.Unmarshal(out, &v))
		assert.True(t, v.Truncated)
		assert.Equal(t, len(big), v.TotalBytes)
		assert.NotEmpty(t, v.Preview)
		assert.True(t, utf8.ValidString(v.Preview))
		assert.True(t, strings.HasPrefix(string(big), v.Preview))
	}
	assert.Nil(t, capResult(nil, 1024))
}
