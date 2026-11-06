//go:build !windows

package engine

import (
	"os"
	"os/exec"
	"syscall"
)

// terminate signals the child's entire process group. setupChild starts the
// server as a group leader in its own group, so the negative-PID signal
// reaches the server plus any descendant it may have spawned, and never the
// parent, which stays in its own group. A group signal that fails (for
// example a platform without group semantics) falls back to signaling the
// server PID alone.
func terminate(p *os.Process) error {
	if err := syscall.Kill(-p.Pid, syscall.SIGTERM); err == nil {
		return nil
	}
	return p.Signal(syscall.SIGTERM)
}

// kill forcefully ends the child's process group after the graceful
// terminate above timed out. Same group-leader precondition as terminate;
// falls back to killing the server PID alone.
func kill(p *os.Process) error {
	if err := syscall.Kill(-p.Pid, syscall.SIGKILL); err == nil {
		return nil
	}
	return p.Kill()
}

// setupChild starts the server as a leader of a fresh process group. The
// group exists so terminate/kill can reach the server and any descendant it
// spawns without ever signaling the parent's group.
func setupChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
