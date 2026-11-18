//go:build windows

package main

// ignoreSIGPIPE does nothing: Windows has no SIGPIPE, writes to a closed
// pipe just fail.
func ignoreSIGPIPE() {}
