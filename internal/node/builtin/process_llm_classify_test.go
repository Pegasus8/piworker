package builtin

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLLMClassifyInfo(t *testing.T) {
	info := LLMClassifyProcessInfo()
	assert.Equal(t, "process-llm-classify", info.Type)
	assert.Equal(t, types.NodeCategoryProcessing, info.Category)
	assert.Equal(t, node.RiskExternal, info.Risk)
	assert.Equal(t, "tags", info.Icon)
	assert.Equal(t, "categories", info.OutputsFrom)
	require.NotNil(t, info.Config)
	cats := info.Config.Properties["categories"]
	assert.Equal(t, "array", cats.Type)
	assert.True(t, cats.AgentVisible)
	assert.ElementsMatch(t, []string{"connection", "categories"}, info.Config.Required)
	assert.Equal(t, "{{payload}}", info.Config.Properties["input"].Default)
	require.Len(t, info.Outputs, 1)
	assert.Equal(t, "other", info.Outputs[0].ID)

	registered, ok := node.DefaultRegistry.Get("process-llm-classify")
	require.True(t, ok)
	assert.Equal(t, "categories", registered.OutputsFrom)
}

func TestLLMClassifyPortsFollowTheCategories(t *testing.T) {
	n, err := NewLLMClassifyProcess(map[string]any{"connection": "c", "categories": []any{"billing", "support"}})
	require.NoError(t, err)
	require.NoError(t, n.Validate())
	in, out := n.Ports()
	assert.Equal(t, LLMClassifyProcessInfo().Inputs, in)
	var ids []string
	for _, p := range out {
		ids = append(ids, p.ID)
	}
	assert.Equal(t, []string{"other", "billing", "support"}, ids)

	// []string, as Go callers write it, works too.
	n, err = NewLLMClassifyProcess(map[string]any{"connection": "c", "categories": []string{"x"}})
	require.NoError(t, err)
	_, out = n.Ports()
	assert.Len(t, out, 2)
}

func TestLLMClassifyRefusesBadCategories(t *testing.T) {
	many := make([]any, 21)
	for i := range many {
		many[i] = fmt.Sprintf("c%d", i)
	}
	for _, tc := range []struct {
		name string
		cats any
		want string
	}{
		{"missing", nil, "categories"},
		{"empty list", []any{}, "categories"},
		{"not a list", "a,b", "categories"},
		{"not text", []any{"a", 3}, "categories"},
		{"empty item", []any{"a", " "}, "empty"},
		{"other", []any{"a", "other"}, `"other"`},
		{"Other", []any{"a", "Other"}, `"Other"`},
		{"error", []any{"error"}, `"error"`},
		{"duplicate", []any{"a", "b", "a"}, `"a" twice`},
		{"too long", []any{strings.Repeat("x", 65)}, "64 characters"},
		{"too many", many, "20 categories"},
		{"surrounding spaces", []any{" a"}, "spaces"},
		{"two lines", []any{"a\nb"}, "one line"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := map[string]any{"connection": "c"}
			if tc.cats != nil {
				cfg["categories"] = tc.cats
			}
			_, err := NewLLMClassifyProcess(cfg)
			assert.ErrorContains(t, err, tc.want)
		})
	}
	// 20 categories of 64 characters are fine.
	ok := make([]any, 20)
	for i := range ok {
		ok[i] = fmt.Sprintf("%02d", i) + strings.Repeat("x", 62)
	}
	_, err := NewLLMClassifyProcess(map[string]any{"connection": "c", "categories": ok})
	assert.NoError(t, err)
}

func TestLLMClassifyRoutesToTheCategory(t *testing.T) {
	answer := `{"category":"billing","confidence":0.92}`
	f := newFakeLLM(t, func(int, string, map[string]any) (int, any) { return 200, chatAnswer(answer, 30, 8) })
	env := f.env(t)
	n, err := NewLLMClassifyProcess(map[string]any{
		"connection": fakeLLMConnection, "categories": []any{"billing", "support"},
		"instructions": "Customer e-mails of {{payload.shop}}.", "input": "{{payload.text}}",
	})
	require.NoError(t, err)
	payload := map[string]any{"shop": "Acme", "text": "My invoice is wrong"}
	in := types.NewMessage(payload, types.DataTypeObject)
	outs, err := n.Process(env.ctx, in)
	require.NoError(t, err)
	require.Len(t, outs, 1)
	assert.Equal(t, "billing", outs[0].SourcePort)
	assert.Equal(t, payload, outs[0].Payload, "the payload is unchanged")
	meta, ok := outs[0].Meta["llm"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "billing", meta["category"])
	assert.Equal(t, 0.92, meta["confidence"])
	assert.Equal(t, fakeLLMConnection, meta["connection"])
	assert.Equal(t, fakeLLMServed, meta["model"])
	assert.Equal(t, int64(30), meta["inputTokens"])

	body := f.Requests()[0]
	assert.EqualValues(t, 256, body["max_tokens"])
	format := body["response_format"].(map[string]any)
	assert.Equal(t, "json_schema", format["type"])
	schema := format["json_schema"].(map[string]any)["schema"].(map[string]any)
	props := schema["properties"].(map[string]any)
	assert.Equal(t, []any{"billing", "support", "other"}, props["category"].(map[string]any)["enum"])
	assert.Equal(t, "number", props["confidence"].(map[string]any)["type"])
	assert.ElementsMatch(t, []any{"category", "confidence"}, schema["required"])
	msgs := requestMessages(t, body)
	require.Len(t, msgs, 2)
	assert.Equal(t, "system", msgs[0]["role"])
	system := msgs[0]["content"].(string)
	assert.Contains(t, system, `"billing"`)
	assert.Contains(t, system, `"other"`)
	assert.Contains(t, system, "Customer e-mails of Acme.")
	assert.Equal(t, map[string]any{"role": "user", "content": "My invoice is wrong"}, msgs[1])

	answer = `{"category":"other","confidence":1.7}`
	outs, err = n.Process(env.ctx, in)
	require.NoError(t, err)
	assert.Equal(t, "other", outs[0].SourcePort)
	assert.Equal(t, 1.0, outs[0].Meta["llm"].(map[string]any)["confidence"], "the confidence is kept within 0..1")

	answer = `{"category":"sales","confidence":0.5}`
	_, err = n.Process(env.ctx, in)
	assert.ErrorIs(t, err, llm.ErrInvalidAnswer, "a category outside the list is an invalid answer")
	assert.False(t, node.IsRetryable(err))
}
