package main

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runMainEnv makes the test binary run main() with the arguments after "--",
// so tests can watch the real process exit.
const runMainEnv = "PIWORKER_MCP_TEST_RUN_MAIN"

func TestMain(m *testing.M) {
	if os.Getenv(runMainEnv) == "1" {
		i := slices.Index(os.Args, "--")
		os.Args = append([]string{"piworker-mcp"}, os.Args[i+1:]...)
		main()
		return
	}
	os.Exit(m.Run())
}

func TestBrokenStdoutExitsWithStatus1(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGPIPE is a Unix signal")
	}
	tokenFile := writeTokenFile(t, testToken, 0o600)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close()) // requests fail at once, and their errors go to stdout

	cmd := exec.Command(os.Args[0], "-test.run=^$", "--", "--url", "http://"+addr+"/mcp", "--token-file", tokenFile)
	cmd.Env = append(os.Environ(), runMainEnv+"=1")
	stdoutR, stdoutW, err := os.Pipe()
	require.NoError(t, err)
	cmd.Stdout = stdoutW
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	// The client goes away: nobody reads the bridge's stdout any more.
	require.NoError(t, stdoutW.Close())
	require.NoError(t, stdoutR.Close())
	_, err = io.WriteString(stdin, `{"jsonrpc":"2.0","id":1,"method":"ping"}`+"\n")
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the bridge kept running without a client")
	}
	_ = stdin.Close()
	var exit *exec.ExitError
	require.True(t, errors.As(err, &exit), "got %v", err)
	assert.Equal(t, 1, exit.ExitCode(), "exit status 1, not killed by SIGPIPE (%v)", exit)
	assert.Contains(t, stderr.String(), "stdout", "the reason is logged")
}
