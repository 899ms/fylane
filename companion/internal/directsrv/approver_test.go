package directsrv

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The approver surface is mounted only when the app hands one over, and then
// on exactly the two paths a paired phone uses. Minting a pairing code is not
// among them: that stays on the local control API.
func TestApproverSurfaceIsMountedOnlyWhenSet(t *testing.T) {
	_, _, bare := newTestServer(t)
	for _, path := range []string{"/approver", "/approver/sw.js", "/v1/approver/inbox", "/v1/approver/claim"} {
		resp, err := http.Get(bare.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s without an approver = %d; want 404", path, resp.StatusCode)
		}
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := Open(context.Background(), t.TempDir(), "test-host", &mcpProbe{}, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	var seen []string
	s.SetApprover(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		w.WriteHeader(http.StatusTeapot)
	}))
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	for _, path := range []string{"/approver", "/approver/sw.js", "/v1/approver/inbox"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusTeapot {
			t.Errorf("%s = %d; want the approver handler's answer", path, resp.StatusCode)
		}
	}
	for _, path := range []string{"/v1/pair", "/v1/devices"} {
		resp, err := http.Post(ts.URL+path, "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s = %d; mounting the approver must not publish pairing", path, resp.StatusCode)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("approver handler saw %v", seen)
	}
}
