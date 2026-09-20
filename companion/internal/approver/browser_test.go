package approver

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestAPhoneCanPairSeeADiffAndApproveInARealBrowser runs the page in a
// headless Chrome against the real handler: the Web Crypto side of the
// envelope and the signed request are checked by the only code that will
// ever run them. It needs a Chrome and a Node; without both it is skipped,
// and the Go-only tests still cover everything on this side of the wire.
func TestAPhoneCanPairSeeADiffAndApproveInARealBrowser(t *testing.T) {
	chrome := os.Getenv("FYLANE_TEST_CHROME")
	if chrome == "" {
		chrome = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	}
	node, err := exec.LookPath("node")
	if _, statErr := os.Stat(chrome); statErr != nil || err != nil {
		t.Skip("needs a Chrome (FYLANE_TEST_CHROME) and node on PATH")
	}

	r := newRig(t)
	r.mu.Lock()
	r.clock = time.Now()
	r.mu.Unlock()
	srv := httptest.NewServer(r.svc.Handler())
	defer srv.Close()
	r.svc.opts.PublicURL = func() string { return srv.URL }
	pairing, err := r.svc.Pair()
	if err != nil {
		t.Fatal(err)
	}
	decided := r.ask("cs_browser")

	cmd := exec.Command(node, "testdata/browser.mjs", chrome, pairing.URL)
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser run: %v\n%s", err, raw)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	var out struct {
		OK                 bool   `json:"ok"`
		Error              string `json:"error"`
		State              string `json:"state"`
		Title              string `json:"title"`
		Command            string `json:"command"`
		Changes            string `json:"changes"`
		DetailHiddenBefore bool   `json:"detailHiddenBefore"`
		DetailHiddenAfter  bool   `json:"detailHiddenAfter"`
		ApproveLabel       string `json:"approveLabel"`
		Device             string `json:"device"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &out); err != nil {
		t.Fatalf("browser output: %v\n%s", err, raw)
	}
	if !out.OK {
		t.Fatalf("browser: %s (state %q)\n%s", out.Error, out.State, raw)
	}
	if !strings.HasPrefix(out.Title, "Claude ") || out.Command != "Update .env.example" {
		t.Errorf("page said %q / %q", out.Title, out.Command)
	}
	if !strings.Contains(out.Changes, secretLine) || !strings.Contains(out.Changes, ".env.example") {
		t.Errorf("details did not show the diff: %q", out.Changes)
	}
	if !out.DetailHiddenBefore || out.DetailHiddenAfter {
		t.Errorf("details must open on the press and not before (hidden before %v, after %v)", out.DetailHiddenBefore, out.DetailHiddenAfter)
	}
	select {
	case d := <-decided:
		if !d.Approved || !strings.HasPrefix(d.Reason, "approver:apr_") {
			t.Fatalf("decision %+v", d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the browser's yes never reached the waiting write")
	}
	devices, _ := r.svc.Devices(t.Context())
	if len(devices) != 1 || devices[0].Name != "Pixel 9" || out.Device != "Pixel 9" {
		t.Errorf("device list %+v, page showed %q", devices, out.Device)
	}
}
