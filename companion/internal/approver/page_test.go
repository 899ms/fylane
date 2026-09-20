package approver

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
	r := newRig(t)
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r.svc.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
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
	ref := regexp.MustCompile(`/approver/icon\.png\?v=([0-9a-f]{8})`)
	inPage, inManifest := ref.FindStringSubmatch(page), ref.FindStringSubmatch(manifest)
	if inPage == nil || inManifest == nil || inPage[1] != inManifest[1] {
		t.Fatalf("page %v and manifest %v must name the same tagged icon", inPage, inManifest)
	}
	if inPage[1] != iconTag {
		t.Errorf("tag %s is not the icon's own %s", inPage[1], iconTag)
	}
	if body := get("/approver/icon.png?v=" + iconTag).Body.Bytes(); len(body) != len(icon) {
		t.Errorf("icon served %d bytes of %d", len(body), len(icon))
	}
	// The decoder is fetched the same way, and only from here: the policy
	// names this origin and nowhere else.
	if !strings.Contains(page, "/approver/jsqr.js?v="+decoderTag) {
		t.Errorf("page does not load the decoder under its tag %s", decoderTag)
	}
	if body := get("/approver/jsqr.js?v=" + decoderTag).Body.Bytes(); len(body) != len(qrDecoder) {
		t.Errorf("decoder served %d bytes of %d", len(body), len(qrDecoder))
	}
	csp := get("/approver").Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self' 'nonce-") || strings.Contains(csp, "http") {
		t.Errorf("policy must allow scripts from this origin and its nonce only: %s", csp)
	}
}
