package cmdexec

import "sync"

// liveGroups is the set of process groups a Runner has running, by workspace.
// It answers one question for page_snapshot: was the process holding a port
// started by a command run in this workspace. A port number alone proves
// nothing — anything on the machine can listen on one — so the answer has to
// come from the processes this Runner put into the world.
type liveGroups struct {
	mu          sync.Mutex
	byWorkspace map[string]map[*procGroup]struct{}
}

func newLiveGroups() *liveGroups {
	return &liveGroups{byWorkspace: map[string]map[*procGroup]struct{}{}}
}

// add records g as running for workspaceID until the returned func is called.
func (l *liveGroups) add(workspaceID string, g *procGroup) (remove func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	set := l.byWorkspace[workspaceID]
	if set == nil {
		set = map[*procGroup]struct{}{}
		l.byWorkspace[workspaceID] = set
	}
	set[g] = struct{}{}
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		delete(l.byWorkspace[workspaceID], g)
		if len(l.byWorkspace[workspaceID]) == 0 {
			delete(l.byWorkspace, workspaceID)
		}
	}
}

// owns reports whether pid belongs to a group running for workspaceID. The
// lock is held across the membership checks so a group cannot be closed —
// its job handle released on Windows — while one is being asked about.
func (l *liveGroups) owns(workspaceID string, pid int) bool {
	if pid <= 1 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for g := range l.byWorkspace[workspaceID] {
		if g.contains(pid) {
			return true
		}
	}
	return false
}

// Owns reports whether process pid was started, directly or by one of its
// descendants, by a command this Runner is still running in workspaceID.
// A process that left its group on purpose (a daemon calling setsid) is not
// owned: it is no longer something a command here is running.
func (r *Runner) Owns(workspaceID string, pid int) bool {
	return r.live.owns(workspaceID, pid)
}
