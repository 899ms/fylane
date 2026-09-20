package approverpage

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// TestTheIconIsInstalledFromAnAddressThatChangesWithIt guards the phone's
// home-screen icon: the page and the manifest must both point at the icon
// under a tag derived from its bytes, with no template marker left behind,
// so a replaced icon is never served from a day-old cache.
func TestTheIconIsInstalledFromAnAddressThatChangesWithIt(t *testing.T) {
	mux := http.NewServeMux()
	Routes(mux)
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d", path, rec.Code)
		}
		return rec
	}
	page := get("/approver").Body.String()
	manifest := get("/approver/manifest.webmanifest").Body.String()
	for name, body := range map[string]string{"page": page, "manifest": manifest} {
		if strings.Contains(body, "{{") {
			t.Errorf("%s still holds a template marker", name)
		}
	}
	ref := regexp.MustCompile(`/approver/icon-([0-9a-f]{8})\.png`)
	inPage, inManifest := ref.FindStringSubmatch(page), ref.FindStringSubmatch(manifest)
	if inPage == nil || inManifest == nil || inPage[1] != inManifest[1] {
		t.Fatalf("page %v and manifest %v must name the same tagged icon", inPage, inManifest)
	}
	if inPage[1] != iconTag {
		t.Errorf("tag %s is not the icon's own %s", inPage[1], iconTag)
	}
	if body := get("/approver/icon-" + iconTag + ".png").Body.Bytes(); len(body) != len(icon) {
		t.Errorf("icon served %d bytes of %d", len(body), len(icon))
	}
	// The decoder is fetched the same way, and only from here: the policy
	// names this origin and nowhere else.
	if !strings.Contains(page, "/approver/jsqr-"+decoderTag+".js") {
		t.Errorf("page does not load the decoder under its tag %s", decoderTag)
	}
	if body := get("/approver/jsqr-" + decoderTag + ".js").Body.Bytes(); len(body) != len(qrDecoder) {
		t.Errorf("decoder served %d bytes of %d", len(body), len(qrDecoder))
	}
	// Every id on the page is one element: a second holder of a section's
	// id is hidden with it, which is how the scan button once vanished.
	ids := regexp.MustCompile(` id="([^"]+)"`).FindAllStringSubmatch(page, -1)
	seen := map[string]bool{}
	for _, m := range ids {
		if seen[m[1]] {
			t.Errorf("id %q is used twice", m[1])
		}
		seen[m[1]] = true
	}
	// The worker must control the page at /approver itself: registered
	// with that scope, and allowed it by the header on its script. With a
	// scope of /approver/ the page is outside it and never sees the worker
	// ready, which is how the notifications row once stayed blank.
	if !strings.Contains(page, `{scope: "/approver"}`) {
		t.Error("the page must register its worker with scope /approver")
	}
	if allowed := get("/approver/sw.js").Header().Get("Service-Worker-Allowed"); allowed != "/approver" {
		t.Errorf("Service-Worker-Allowed is %q, want /approver", allowed)
	}
	if !strings.Contains(manifest, `"scope": "/approver"`) {
		t.Error("the manifest scope must match")
	}
	csp := get("/approver").Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self' 'nonce-") || strings.Contains(csp, "http") {
		t.Errorf("policy must allow scripts from this origin and its nonce only: %s", csp)
	}
	// Under a different path the page is not there: the handler is
	// registered for /approver/ as a prefix and must not answer /approver/x.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/approver/other", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /approver/other: %d, want 404", rec.Code)
	}
}
