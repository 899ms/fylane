//go:build !windows

package cmdexec

import (
	"context"
	"os"
	"testing"
	"time"
)

// liveGroupsFor waits for workspaceID to have a running group and returns
// one, or fails.
func liveGroupsFor(t *testing.T, r *Runner, workspaceID string) *procGroup {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.live.mu.Lock()
		for g := range r.live.byWorkspace[workspaceID] {
			r.live.mu.Unlock()
			return g
		}
		r.live.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no running group for %s", workspaceID)
	return nil
}

func TestTheRunnerOwnsWhatItIsRunningAndNothingElse(t *testing.T) {
	r, _ := newRunner(t)
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run(ctx, Spec{WorkspaceID: "ws_a", Root: root, Argv: []string{"sleep", "30"}, Timeout: time.Minute})
	}()
	g := liveGroupsFor(t, r, "ws_a")

	if !r.Owns("ws_a", g.pgid) {
		t.Fatal("the command's own process is not owned by its workspace")
	}
	if r.Owns("ws_b", g.pgid) {
		t.Fatal("another workspace owns a process it did not start")
	}
	if r.Owns("ws_a", os.Getpid()) {
		t.Fatal("the Core itself is owned by a command")
	}
	if r.Owns("ws_a", 1) || r.Owns("ws_a", 0) {
		t.Fatal("pid 0 or 1 is owned")
	}

	cancel()
	<-done
	if r.Owns("ws_a", g.pgid) {
		t.Fatal("a finished command still owns its process")
	}
	r.live.mu.Lock()
	left := len(r.live.byWorkspace)
	r.live.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d workspaces left in the live set", left)
	}
}
