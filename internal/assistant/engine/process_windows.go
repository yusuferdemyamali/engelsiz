package engine

import (
	"os"
	"os/exec"
)

// terminate ends the server process. There is no job object here: only the
// server PID itself is signaled, and no descendant tree is managed or
// protected on this platform. Policy: the configured server is expected to
// run as a single process; operators must not point the engine at a
// supervising wrapper that outlives its PID.
func terminate(p *os.Process) error { return p.Kill() }

// kill is the escalation step after terminate timed out. Same single-PID
// scope and single-process policy as terminate.
func kill(p *os.Process) error { return p.Kill() }

// setupChild performs no Windows-specific process setup. Stdout/stderr are
// already piped to bounded discarding counters and no console changes are
// needed. In particular no job object is created, so no tree-level
// termination guarantee is claimed on this platform.
func setupChild(cmd *exec.Cmd) {}
