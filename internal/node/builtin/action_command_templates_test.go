//go:build unix

package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/require"
)

func TestCommandTemplateValuesAreLiteral(t *testing.T) {
	for _, placeholder := range []string{"{{payload}}", "{{topic}}", "{{meta.input}}"} {
		for _, quote := range []string{"", "\"", "'"} {
			t.Run(placeholder+quote, func(t *testing.T) {
				marker := filepath.Join(t.TempDir(), "injected")
				values := []string{
					"$(touch " + marker + ")", "`touch " + marker + "`",
					"\"; touch " + marker + "; #", "'; touch " + marker + "; #",
					"a b * ? [x] \\ $HOME\nsecond line", "{{topic}} {{meta.input}}", "",
				}
				for _, value := range values {
					action, err := NewCommandAction(map[string]interface{}{"command": "printf '%s' " + quote + placeholder + quote})
					require.NoError(t, err)
					msg := types.NewMessage(value, types.DataTypeString)
					msg.Topic = value
					msg.SetMeta("input", value)
					outputs, err := action.Process(context.Background(), msg)
					require.NoError(t, err)
					result := outputs[0].Payload.(map[string]interface{})
					require.Equal(t, true, result["success"], result)
					require.Equal(t, value, result["stdout"])
					_, err = os.Stat(marker)
					require.ErrorIs(t, err, os.ErrNotExist, "a placeholder executed shell code")
				}
			})
		}
	}
}

func TestCommandTemplateRejectsReinterpretedContextsBeforeExecuting(t *testing.T) {
	for _, suffix := range []string{
		"echo $(printf '%s' {{payload}})", "echo `printf '%s' {{payload}}`",
		`echo $'{{payload}}'`, `echo $"{{payload}}"`,
		`echo "${HOME:-{{payload}}}"`, `cat <<< {{payload}}`, `[[ {{payload}} == x ]]`,
		"echo $(({{payload}}))", "echo ${HOME:-{{payload}}}",
		"cat <<EOF\n{{payload}}\nEOF", "cat <<'EOF'\n{{payload}}\nEOF",
		`printf '%s' \{{payload}}`, `printf '%s' "\{{payload}}"`,
	} {
		t.Run(suffix, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "should-not-run")
			action, err := NewCommandAction(map[string]interface{}{"command": "touch '" + marker + "'; " + suffix})
			require.NoError(t, err)
			msg := types.NewMessage("unsafe", types.DataTypeString)
			_, err = action.Process(context.Background(), msg)
			require.Error(t, err)
			require.Error(t, action.Validate(), "deployment must reject unsupported template contexts")
			_, err = os.Stat(marker)
			require.ErrorIs(t, err, os.ErrNotExist, "validation must happen before any command executes")
		})
	}
}

func TestCommandTemplateDoesNotExpandDataAgain(t *testing.T) {
	action, err := NewCommandAction(map[string]interface{}{"command": `printf '%s|%s|%s' "pre{{payload}}post" {{topic}} '{{meta.input}}'`})
	require.NoError(t, err)
	msg := types.NewMessage("{{topic}}", types.DataTypeString)
	msg.Topic = "{{meta.input}}"
	msg.SetMeta("input", "$(printf injected)")
	outputs, err := action.Process(context.Background(), msg)
	require.NoError(t, err)
	result := outputs[0].Payload.(map[string]interface{})
	require.Equal(t, true, result["success"])
	require.Equal(t, "pre{{topic}}post|{{meta.input}}|$(printf injected)", result["stdout"])
}

func TestCommandTemplateRejectsNULWithoutSideEffects(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "should-not-run")
	action, err := NewCommandAction(map[string]interface{}{"command": "touch '" + marker + "'; printf '%s' {{payload}}"})
	require.NoError(t, err)
	_, err = action.Process(context.Background(), types.NewMessage("a\x00b", types.DataTypeString))
	require.Error(t, err)
	_, err = os.Stat(marker)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCommandTemplatePreservesBytePayloadAndManyBindings(t *testing.T) {
	action, err := NewCommandAction(map[string]interface{}{"command": `printf '%s' {{payload}}{{payload}}{{payload}}{{payload}}{{payload}}{{payload}}{{payload}}{{payload}}{{payload}}{{payload}}{{payload}}`})
	require.NoError(t, err)
	outputs, err := action.Process(context.Background(), types.NewMessage([]byte("x"), types.DataTypeString))
	require.NoError(t, err)
	require.Equal(t, "xxxxxxxxxxx", outputs[0].Payload.(map[string]interface{})["stdout"])
}

func TestCommandTemplateOrdinaryShellStructure(t *testing.T) {
	for _, command := range []string{
		`value={{payload}}; printf '%s' "$value"`,
		`if true; then printf '%s' "prefix{{payload}}suffix"; fi`,
		`(printf '%s' '{{payload}}')`,
	} {
		t.Run(command, func(t *testing.T) {
			action, err := NewCommandAction(map[string]interface{}{"command": command})
			require.NoError(t, err)
			require.NoError(t, action.Validate())
			outputs, err := action.Process(context.Background(), types.NewMessage("literal $HOME", types.DataTypeString))
			require.NoError(t, err)
			result := outputs[0].Payload.(map[string]interface{})
			require.Equal(t, true, result["success"])
			expected := "literal $HOME"
			if command[0:2] == "if" {
				expected = "prefix" + expected + "suffix"
			}
			require.Equal(t, expected, result["stdout"])
			require.Equal(t, command, result["command"])
		})
	}
}
