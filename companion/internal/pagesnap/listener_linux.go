//go:build linux

package pagesnap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const supported = true

// tcpListen is the state column's value for a listening socket in
// /proc/net/tcp.
const tcpListen = "0A"

// listenerPIDs reads the socket tables and matches their inodes against the
// file descriptors of the processes this user can see.
func listenerPIDs(ctx context.Context, port int) ([]int, error) {
	inodes := map[string]bool{}
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(table)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", table, err)
		}
		for ino := range listeningInodes(string(data), port) {
			inodes[ino] = true
		}
	}
	if len(inodes) == 0 {
		return nil, nil
	}

	procs, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("reading /proc: %w", err)
	}
	var pids []int
	for _, p := range procs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		pid, err := strconv.Atoi(p.Name())
		if err != nil {
			continue
		}
		fdDir := filepath.Join("/proc", p.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil || !strings.HasPrefix(link, "socket:[") {
				continue
			}
			if inodes[strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")] {
				pids = append(pids, pid)
				break
			}
		}
	}
	return pids, nil
}

// listeningInodes returns the inodes of sockets listening on port in one
// /proc/net/tcp table: local address is ADDR:PORT in hex, state is the fourth
// column and the inode the tenth.
func listeningInodes(table string, port int) map[string]bool {
	out := map[string]bool{}
	lines := strings.Split(table, "\n")
	for _, line := range lines[min(1, len(lines)):] {
		f := strings.Fields(line)
		if len(f) < 10 || f[3] != tcpListen {
			continue
		}
		i := strings.LastIndexByte(f[1], ':')
		if i < 0 {
			continue
		}
		p, err := strconv.ParseUint(f[1][i+1:], 16, 16)
		if err != nil || int(p) != port {
			continue
		}
		out[f[9]] = true
	}
	return out
}
