package approver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBehindARelayEveryCredentialCarriesTheRouteKey: a Companion behind a
// relay prefixes what it hands out with its relay device id, the relay
// routes on that prefix and nothing else, and the Companion looks the
// prefixed id up as it is. A code that lost its prefix is nobody's.
func TestBehindARelayEveryCredentialCarriesTheRouteKey(t *testing.T) {
	r := newRig(t)
	opts := r.svc.opts
	opts.RouteKey = "dev_abc"
	svc, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	do := func(req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		svc.Handler().ServeHTTP(rec, req)
		return rec
	}

	pairing, err := svc.Pair()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pairing.Code, "dev_abc.") || pairing.URL != "https://core.example/approver#"+pairing.Code {
		t.Fatalf("pairing %+v does not carry the route key", pairing)
	}
	p := newPhone(t)
	bare := p.claim()
	bare.Code = strings.TrimPrefix(pairing.Code, "dev_abc.")
	if rec := do(httptest.NewRequest("POST", "/v1/approver/claim", jsonBody(bare))); rec.Code != http.StatusNotFound {
		t.Fatalf("a code without its prefix claimed: %d %s", rec.Code, rec.Body)
	}
	req := p.claim()
	req.Code = pairing.Code
	rec := do(httptest.NewRequest("POST", "/v1/approver/claim", jsonBody(req)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("claim: %d %s", rec.Code, rec.Body)
	}
	var res ClaimResponse
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.DeviceID, "dev_abc.apr_") {
		t.Fatalf("device id %q does not carry the route key", res.DeviceID)
	}
	p.id = res.DeviceID
	// The prefixed id is the id: a signed request under it is the phone's.
	if rec := do(p.signed("GET", "/v1/approver/inbox?wait=0", nil, r.now(), "")); rec.Code != http.StatusOK {
		t.Fatalf("inbox under the prefixed id: %d %s", rec.Code, rec.Body)
	}
	// Without a route key nothing is prefixed: direct mode is unchanged.
	if plain, err := r.svc.Pair(); err != nil || strings.Contains(plain.Code, ".") {
		t.Errorf("direct-mode code %q, %v", plain.Code, err)
	}
	// A key holding a separator would route or parse wrong; it is refused
	// when the service is built, not discovered on the phone.
	for _, bad := range []string{"dev.x", "dev:x", "dev x"} {
		opts.RouteKey = bad
		if _, err := New(opts); err == nil {
			t.Errorf("route key %q accepted", bad)
		}
	}
}

// TestATypedCodeIsForgivenItsCaseAndDashes: the code is typed when the
// camera cannot be used, and what a thumb produces is lower case with the
// groups run together or the dashes kept. The route key before the dot is
// a machine id and stays exact.
func TestATypedCodeIsForgivenItsCaseAndDashes(t *testing.T) {
	r := newRig(t)
	opts := r.svc.opts
	opts.RouteKey = "dev_Abc"
	svc, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	do := func(req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		svc.Handler().ServeHTTP(rec, req)
		return rec
	}
	pairing, err := svc.Pair()
	if err != nil {
		t.Fatal(err)
	}
	shown := strings.TrimPrefix(pairing.Code, "dev_Abc.")
	typed := "dev_Abc." + strings.ToLower(strings.ReplaceAll(shown, "-", ""))
	req := newPhone(t).claim()
	req.Code = typed
	if rec := do(httptest.NewRequest("POST", "/v1/approver/claim", jsonBody(req))); rec.Code != http.StatusCreated {
		t.Fatalf("typed %q for shown %q: %d %s", typed, pairing.Code, rec.Code, rec.Body)
	}
	// The key is not forgiven: a different case is a different machine.
	again, _ := svc.Pair()
	req = newPhone(t).claim()
	req.Code = strings.ToLower(again.Code)
	if rec := do(httptest.NewRequest("POST", "/v1/approver/claim", jsonBody(req))); rec.Code != http.StatusNotFound {
		t.Fatalf("a lower-cased route key claimed: %d", rec.Code)
	}
}
