//go:build darwin || linux

package pagesnap

import (
	"context"
	"net"
	"os"
	"slices"
	"testing"
)

func TestListenerPIDsNamesTheProcessOnAPort(t *testing.T) {
	for _, network := range []string{"tcp4", "tcp6"} {
		addr := "127.0.0.1:0"
		if network == "tcp6" {
			addr = "[::1]:0"
		}
		ln, err := net.Listen(network, addr)
		if err != nil {
			t.Logf("%s: %v", network, err)
			continue
		}
		port := ln.Addr().(*net.TCPAddr).Port
		pids, err := ListenerPIDs(context.Background(), port)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(pids, os.Getpid()) {
			t.Fatalf("%s port %d: listeners %v do not include this process %d", network, port, pids, os.Getpid())
		}

		ln.Close()
		pids, err = ListenerPIDs(context.Background(), port)
		if err != nil {
			t.Fatal(err)
		}
		if slices.Contains(pids, os.Getpid()) {
			t.Fatalf("%s port %d: a closed listener is still named", network, port)
		}
	}
}
