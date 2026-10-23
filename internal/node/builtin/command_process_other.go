//go:build !unix

package builtin

import "os/exec"

// Other platforms retain exec.CommandContext's direct-process cancellation.
func isolateCommand(cmd *exec.Cmd) {}
