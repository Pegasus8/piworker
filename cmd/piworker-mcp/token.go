package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	// tokenPrefix starts every PiWorker agent token (pwk_<id>_<secret>).
	tokenPrefix = "pwk_"
	// maxTokenFileBytes bounds what is read from --token-file.
	maxTokenFileBytes = 4096
	// maxTokenLength bounds the token itself. Real tokens are 73 characters;
	// the exact shape is checked by the server.
	maxTokenLength = 512
)

// readToken reads the agent token from path. The token never comes from the
// command line (other users can see process arguments) and is never echoed:
// errors describe the file, not its content. On Unix the file must not be
// accessible to group or others, as ssh requires for private keys.
func readToken(path string) (string, error) {
	if path == "" {
		return "", errors.New("--token-file is required: the path of a file holding the agent token (pwk_…) created under Agents in the PiWorker editor")
	}
	path, err := expandHome(path)
	if err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("token file %s does not exist; save the agent token there (see piworker-mcp --help)", path)
	}
	if err != nil {
		return "", fmt.Errorf("cannot open token file %s: %w", path, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("cannot read token file %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("token file %s is not a regular file", path)
	}
	if err := checkTokenFilePermissions(path, info); err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(f, maxTokenFileBytes+1))
	if err != nil {
		return "", fmt.Errorf("cannot read token file %s: %w", path, err)
	}
	if len(data) > maxTokenFileBytes {
		return "", fmt.Errorf("token file %s is too large: it must hold only the agent token (pwk_…)", path)
	}
	token := strings.TrimSpace(string(data))
	switch {
	case token == "":
		return "", fmt.Errorf("token file %s is empty: save the agent token (pwk_…) in it", path)
	case !strings.HasPrefix(token, tokenPrefix):
		return "", fmt.Errorf("token file %s does not hold a PiWorker agent token: it must start with %s", path, tokenPrefix)
	case len(token) > maxTokenLength || !isTokenText(token):
		return "", fmt.Errorf("token file %s does not hold a valid agent token: it must contain only the token (pwk_ followed by letters, digits and underscores) on one line", path)
	}
	return token, nil
}

// isTokenText reports whether s only has characters a token can have, so it
// can never smuggle anything into the Authorization header.
func isTokenText(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

// expandHome replaces a leading "~" with the user's home directory, since
// Claude Desktop starts the bridge without a shell.
func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot expand ~ in %s: %w", path, err)
	}
	return filepath.Join(home, path[1:]), nil
}
