//go:build !windows

package main

import (
	"fmt"
	"os"
)

// checkTokenFilePermissions refuses a token file that group or others may
// read, write or execute.
func checkTokenFilePermissions(path string, info os.FileInfo) error {
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("token file %s is accessible to other users (permissions %#o); restrict it with: chmod 600 %s", path, perm, path)
	}
	return nil
}
