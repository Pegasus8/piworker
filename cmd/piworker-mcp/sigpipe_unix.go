//go:build !windows

package main

import (
	"os/signal"
	"syscall"
)

// ignoreSIGPIPE makes a write to a closed stdout fail with EPIPE instead of
// killing the bridge, so it logs why it stops and exits with status 1.
func ignoreSIGPIPE() { signal.Ignore(syscall.SIGPIPE) }
