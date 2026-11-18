package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// goList runs go list on this package (not its tests) with extra env.
func goList(t *testing.T, env []string, args ...string) string {
	t.Helper()
	goTool, err := exec.LookPath("go")
	if err != nil {
		goTool = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	cmd := exec.Command(goTool, append([]string{"list"}, append(args, ".")...)...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "go list: %s", out)
	return string(out)
}

// The bridge ships as CGO_ENABLED=0 binaries for macOS, Windows and Linux, so
// nothing it imports may need cgo: in particular not PiWorker's storage
// (mattn/go-sqlite3).
func TestBridgeIsPureGo(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	forbidden := []string{
		"github.com/mattn/go-sqlite3",
		"github.com/Pegasus8/piworker/internal/storage",
	}
	// Each release platform has its own files (token_windows.go, …).
	for _, goos := range []string{"linux", "darwin", "windows"} {
		deps := goList(t, []string{"GOOS=" + goos, "CGO_ENABLED=1"}, "-deps", "-f", `{{.ImportPath}}{{"\t"}}{{.Standard}}{{"\t"}}{{join .CgoFiles " "}}`)
		for _, line := range strings.Split(strings.TrimSpace(deps), "\n") {
			path, rest, _ := strings.Cut(line, "\t")
			standard, cgoFiles, _ := strings.Cut(rest, "\t")
			for _, f := range forbidden {
				require.NotEqual(t, f, path, "piworker-mcp must not import %s (cgo) on %s", f, goos)
			}
			if standard == "false" {
				require.Empty(t, cgoFiles, "piworker-mcp imports %s, which uses cgo on %s", path, goos)
			}
		}

		// And every package resolves without cgo.
		broken := goList(t, []string{"GOOS=" + goos, "CGO_ENABLED=0"}, "-e", "-deps", "-f", `{{with .Error}}{{.}}{{"\n"}}{{end}}{{range .DepsErrors}}{{.}}{{"\n"}}{{end}}`)
		require.Empty(t, strings.TrimSpace(broken), "piworker-mcp does not build with CGO_ENABLED=0 for %s", goos)
	}
}
