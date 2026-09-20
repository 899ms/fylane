package approver

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestAPhoneCanForgetItsOwnPairingAndNoOthers: the phone's forget reaches
// the computer's list; it takes a signature, and it withdraws one pairing.
func TestAPhoneCanForgetItsOwnPairingAndNoOthers(t *testing.T) {
	r := newRig(t)
	phone, other := newPhone(t), newPhone(t)
	r.pair(phone)
	r.pair(other)

	if rec := r.do(httptest.NewRequest("POST", "/v1/approver/forget", bytes.NewReader([]byte("{}")))); rec.Code != http.StatusUnauthorized {
		t.Fatalf("an unsigned forget must be refused: %d", rec.Code)
	}
	if devices, _ := r.svc.Devices(t.Context()); len(devices) != 2 {
		t.Fatalf("a refused forget must change nothing: %d devices", len(devices))
	}

	rec := r.do(phone.signed("POST", "/v1/approver/forget", []byte("{}"), r.now(), ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("forget: %d %s", rec.Code, rec.Body)
	}
	devices, _ := r.svc.Devices(t.Context())
	if len(devices) != 1 || devices[0].ID != other.id {
		t.Fatalf("exactly the forgetting phone must be gone, list is %+v", devices)
	}
	// The forgotten phone is now a stranger; the other still gets in.
	if rec := r.do(phone.signed("GET", "/v1/approver/inbox", nil, r.now().Add(time.Second), "")); rec.Code != http.StatusUnauthorized {
		t.Errorf("a forgotten phone must be refused: %d", rec.Code)
	}
	if rec := r.do(other.signed("GET", "/v1/approver/inbox", nil, r.now().Add(time.Second), "")); rec.Code != http.StatusOK {
		t.Errorf("the other phone must be unaffected: %d", rec.Code)
	}
}
