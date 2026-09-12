//go:build unix

package builtin

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// isolateCommand puts the shell and its children in a separate process group.
// Killing only the shell can leave children running and holding output pipes
// open, which prevents Cmd.Run from returning after cancellation.
func isolateCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
