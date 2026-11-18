package toolschema

import (
	"go/build"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// internal/node imports this package (node.ToolSpec), so it must not import
// node or flow itself.
func TestNoNodeOrFlowImports(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	require.NoError(t, err)
	for _, imp := range pkg.Imports {
		if strings.HasPrefix(imp, "github.com/Pegasus8/piworker/") {
			t.Errorf("toolschema imports %s; it must stay free of PiWorker packages", imp)
		}
	}
}
