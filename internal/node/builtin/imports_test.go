package builtin

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Nodes depend on the node contracts (internal/node), never on the runtime
// that runs them: internal/flow imports node, and a builtin importing flow
// would tie every node to the runtime's internals. Tests may (they deploy
// real flows).
func TestBuiltinNodesDoNotImportTheRuntime(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		require.NoError(t, err)
		for _, imp := range f.Imports {
			require.NotEqual(t, `"github.com/Pegasus8/piworker/internal/flow"`, imp.Path.Value,
				"%s imports internal/flow; use the contracts in internal/node", name)
		}
	}
}
