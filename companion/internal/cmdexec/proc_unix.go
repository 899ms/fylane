//go:build !windows

package cmdexec

import (
	osexec "os/exec"
	"syscall"
	"time"
)

// procGroup puts the child in its own process group so a timeout can reach
// everything it spawned. Build tools fork compilers, test runners, and
// watchers; signalling only the direct child leaves those running and still
// holding the output pipe open, which is the classic "it exited but Wait
// never returned" hang.
type procGroup struct {
	// pgid is the group the child leads, set once it has started.
	pgid int
}

func newProcGroup() *procGroup { return &procGroup{} }

func (g *procGroup) prepare(cmd *osexec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// adopt only notes the group: Setpgid took effect at fork time, so the group
// exists before the child can spawn anything, and the child leads it.
func (g *procGroup) adopt(cmd *osexec.Cmd) error {
	if cmd.Process != nil {
		g.pgid = cmd.Process.Pid
	}
	return nil
}

// contains reports whether pid is in this group. A descendant that moved to a
// group of its own is not.
func (g *procGroup) contains(pid int) bool {
	if g.pgid <= 1 {
		return false
	}
	pgid, err := syscall.Getpgid(pid)
	return err == nil && pgid == g.pgid
}

// kill asks the whole group to leave, then makes it. The grace period is not
// politeness: a compiler killed between writing an object file and renaming
// it leaves a corrupt artifact that the next build trusts.
func (g *procGroup) kill(cmd *osexec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	// A pgid of 0 or 1 would turn kill(-pgid) into "signal every process
	// this user owns". If the group cannot be identified, settle for the
	// one process we are certain about.
	if err != nil || pgid <= 1 {
		return cmd.Process.Kill()
	}
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
		return syscall.Kill(-pgid, syscall.SIGKILL)
	}
	time.AfterFunc(killGrace, func() { syscall.Kill(-pgid, syscall.SIGKILL) })
	return nil
}

func (g *procGroup) close() {}
