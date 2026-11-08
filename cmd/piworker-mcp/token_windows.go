//go:build windows

package main

import "os"

// checkTokenFilePermissions does nothing on Windows: access is governed by
// ACLs, which Go does not expose. The README shows how to restrict the file
// to the current user with icacls.
func checkTokenFilePermissions(string, os.FileInfo) error { return nil }
