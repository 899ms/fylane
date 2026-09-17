package authsrv

import (
	"net/http"
	"testing"
)

// The hook exists so the machine can say a platform must authorize again.
// It fires on the path where that is true, and only there.
func TestTheRevocationHookFiresWhenTheFamilyIsActuallyGone(t *testing.T) {
	store := NewMemoryStore()
	raw := seedRefresh(t, store, "fam_hook_ok", true)

	var calls []string
	s := &Server{Store: store, OnFamilyRevoked: func(clientID string) {
		calls = append(calls, clientID)
	}}

	status, body := postRefresh(t, s, raw)
	if status != http.StatusBadRequest {
		t.Fatalf("replayed token = %d %s, want 400", status, body)
	}
	if len(calls) != 1 {
		t.Fatalf("hook called %d times, want once", len(calls))
	}
	// The client id is what the caller needs to name the platform; a hook
	// handed the family or the device instead would compile and be useless.
	if calls[0] != "cl_1" {
		t.Fatalf("hook was told %q, want the client the replayed token belonged to", calls[0])
	}
}

// A store that cannot revoke leaves the family live. Saying otherwise would
// mark a platform as needing to reconnect while its sessions still work —
// and send the user to re-authorize a connection nothing broke.
func TestTheRevocationHookStaysQuietWhenNothingWasRevoked(t *testing.T) {
	store := writeFailStore{MemoryStore: NewMemoryStore(), failRevoke: true}
	raw := seedRefresh(t, store, "fam_hook_fail", true)

	called := 0
	s := &Server{Store: store, OnFamilyRevoked: func(string) { called++ }}

	status, body := postRefresh(t, s, raw)
	if status != http.StatusBadRequest {
		t.Fatalf("replayed token = %d %s, want 400", status, body)
	}
	if called != 0 {
		t.Fatalf("hook fired %d times for a family that is still live", called)
	}
}

// A server with no hook is the relay's configuration, and the one this
// package shipped with before the hook existed.
func TestReuseWithoutAHookIsUnchanged(t *testing.T) {
	store := NewMemoryStore()
	raw := seedRefresh(t, store, "fam_hook_nil", true)

	status, body := postRefresh(t, &Server{Store: store}, raw)
	if status != http.StatusBadRequest {
		t.Fatalf("replayed token = %d %s, want 400", status, body)
	}
}
