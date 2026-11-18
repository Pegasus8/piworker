package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIDKeyMatchesTheSameIDOnly(t *testing.T) {
	key := func(raw string) string {
		k, ok := idKey(json.RawMessage(raw))
		require.True(t, ok, raw)
		return k
	}
	assert.Equal(t, key(`7`), key(`7.0`))
	assert.Equal(t, key(`7`), key(`7e0`))
	assert.NotEqual(t, key(`7`), key(`"7"`), "a string id is not the number")
	assert.NotEqual(t, key(`1.5`), key(`1`))
	assert.Equal(t, key(`"a b"`), key(` "a b" `))

	for _, bad := range []string{``, `null`, `true`, `{}`, `[]`, `1e400`} {
		_, ok := idKey(json.RawMessage(bad))
		assert.False(t, ok, bad)
	}
}

func TestHostPortDefaults(t *testing.T) {
	assert.Equal(t, "pi.local:443", hostPort(mustURL(t, "https://pi.local/mcp")))
	assert.Equal(t, "127.0.0.1:80", hostPort(mustURL(t, "http://127.0.0.1/mcp")))
	assert.Equal(t, "[::1]:8443", hostPort(mustURL(t, "https://[::1]:8443/mcp")))
}

func TestReadLineBoundsAndRecovers(t *testing.T) {
	br := bufio.NewReaderSize(strings.NewReader("short\r\n"+strings.Repeat("x", 100)+"\nnext\nlast"), 16)
	line, err := readLine(br, 10)
	require.NoError(t, err)
	assert.Equal(t, "short", string(line))
	_, err = readLine(br, 10)
	assert.True(t, errors.Is(err, errLineTooLong))
	line, err = readLine(br, 10)
	require.NoError(t, err)
	assert.Equal(t, "next", string(line))
	line, err = readLine(br, 10)
	require.NoError(t, err)
	assert.Equal(t, "last", string(line), "a last line without newline")
	_, err = readLine(br, 10)
	assert.ErrorIs(t, err, io.EOF)
}
