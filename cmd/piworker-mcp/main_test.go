package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(context.Background(), args, strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestRunPrintsTheVersion(t *testing.T) {
	code, stdout, stderr := runCLI(t, "--version")
	assert.Equal(t, 0, code)
	assert.Equal(t, "piworker-mcp "+version+"\n", stdout)
	assert.Empty(t, stderr)
}

func TestRunPrintsHelp(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		code, stdout, stderr := runCLI(t, flag)
		assert.Equal(t, 0, code)
		for _, want := range []string{"--url", "--token-file", "--ca-fingerprint", "--ca-file", "--timeout", "--version", "stderr"} {
			assert.Contains(t, stdout, want)
		}
		assert.Empty(t, stderr)
	}
}

func TestRunNeverTakesTheTokenOnTheCommandLine(t *testing.T) {
	for _, args := range [][]string{
		{"--token", testToken},
		{"--token=" + testToken},
		{"--url", "https://pi.local:8443/mcp", testToken},
		{"--url", "https://pi.local:8443/mcp", "--token-file", "x", "--", testToken},
	} {
		code, stdout, stderr := runCLI(t, args...)
		assert.Equal(t, 2, code, args)
		assert.Empty(t, stdout)
		assert.NotContains(t, stderr, testToken, "the token is never echoed")
		assert.Contains(t, stderr, "--token-file")
	}
}

func TestRunValidatesFlags(t *testing.T) {
	tokenFile := writeTokenFile(t, testToken, 0o600)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "--url is required"},
		{[]string{"--url", "https://pi.local:8443/mcp"}, "--token-file is required"},
		{[]string{"--url", "http://pi.local:8080/mcp", "--token-file", tokenFile}, "plain http"},
		{[]string{"--url", "https://pi.local:8443/mcp", "--token-file", tokenFile, "--ca-fingerprint", "SHA256:AB"}, "invalid --ca-fingerprint"},
		{[]string{"--url", "https://pi.local:8443/mcp", "--token-file", tokenFile, "--timeout", "0s"}, "--timeout"},
		{[]string{"--url", "https://pi.local:8443/mcp", "--token-file", tokenFile, "--ca-file", filepath.Join(t.TempDir(), "none")}, "--ca-file"},
		{[]string{"--bogus"}, "-bogus"},
	} {
		code, stdout, stderr := runCLI(t, tc.args...)
		assert.Equal(t, 2, code, tc.args)
		assert.Empty(t, stdout, "stdout carries only protocol messages")
		assert.Contains(t, stderr, tc.want, tc.args)
		assert.Contains(t, stderr, "piworker-mcp --help", "points to the help")
	}
}

func TestRunRelaysUntilStdinCloses(t *testing.T) {
	f := newSDKFixture(t, true)
	tokenFile := writeTokenFile(t, testToken+"\n", 0o600)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	stderr := &syncBuffer{}
	exit := make(chan int, 1)
	go func() {
		code := run(context.Background(), []string{"--url", f.url, "--token-file", tokenFile, "--ca-fingerprint", f.ca.fingerprint()}, inR, outW, stderr)
		_ = outW.Close()
		exit <- code
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	session, err := client.Connect(testCtx(t), &mcp.IOTransport{Reader: outR, Writer: inW}, nil)
	require.NoError(t, err)
	res, err := session.CallTool(testCtx(t), &mcp.CallToolParams{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"echo": "hi"}, res.StructuredContent)
	require.NoError(t, session.Close())

	select {
	case code := <-exit:
		assert.Equal(t, 0, code)
	case <-time.After(10 * time.Second):
		t.Fatal("the bridge did not exit when stdin closed")
	}
	log := stderr.String()
	assert.Contains(t, log, f.url)
	assert.Contains(t, log, f.ca.fingerprint())
	assert.NotContains(t, log, testToken)
}

func TestRunRefusesAnOpenTokenFile(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("Unix permissions only")
	}
	tokenFile := writeTokenFile(t, testToken, 0o644)
	code, stdout, stderr := runCLI(t, "--url", "https://pi.local:8443/mcp", "--token-file", tokenFile)
	assert.Equal(t, 2, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "chmod 600")
	assert.NotContains(t, stderr, testToken)
}
