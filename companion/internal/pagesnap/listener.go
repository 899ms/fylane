// Package pagesnap takes a picture of a page a workspace's own dev server is
// serving on this machine, for page_snapshot.
//
// Three things make it narrower than "open a browser on localhost", and each
// is the reason the tool exists in this shape rather than not at all:
//
//   - The port must be held by a process a command in the same workspace
//     started. A port number alone proves nothing; anything on the machine can
//     listen on one.
//   - Every request the page makes goes through a proxy that allows the
//     workspace's own ports, public addresses when the workspace may use the
//     network, and nothing else — no other loopback port, no private network.
//   - The browser gets a fresh profile that is deleted afterwards, so none of
//     the user's cookies or logins come along.
package pagesnap

import (
	"context"
	"errors"
)

// ErrUnsupported means this platform has no way here to name the process
// holding a port, so ownership cannot be checked and nothing is taken.
var ErrUnsupported = errors.New("finding the process on a port is not supported on this platform")

// Supported reports whether this platform can name the process on a port,
// which a snapshot cannot be taken without.
func Supported() bool { return supported }

// ListenerPIDs returns the processes listening on TCP port on this machine,
// on any local address. An empty result with a nil error means nothing is
// listening.
func ListenerPIDs(ctx context.Context, port int) ([]int, error) {
	return listenerPIDs(ctx, port)
}
