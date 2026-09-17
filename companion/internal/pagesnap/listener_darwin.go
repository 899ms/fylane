//go:build darwin

package pagesnap

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	osexec "os/exec"
	"strconv"
)

const supported = true

// listenerPIDs asks lsof, which ships with macOS at a fixed path. Without
// root it sees only this user's processes, which is the set a dev server
// started from this Companion belongs to.
func listenerPIDs(ctx context.Context, port int) ([]int, error) {
	out, err := osexec.CommandContext(ctx, "/usr/sbin/lsof", "-nP", "-a",
		"-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fp").Output()
	if err != nil {
		// lsof exits 1 when nothing matched, which is an answer, not a failure.
		var exit *osexec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 && len(bytes.TrimSpace(out)) == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("listing listeners on port %d: %w", port, err)
	}
	return parseLsofPIDs(out), nil
}

func parseLsofPIDs(out []byte) []int {
	seen := map[int]bool{}
	var pids []int
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if len(line) < 2 || line[0] != 'p' {
			continue
		}
		pid, err := strconv.Atoi(line[1:])
		if err != nil || seen[pid] {
			continue
		}
		seen[pid] = true
		pids = append(pids, pid)
	}
	return pids
}
