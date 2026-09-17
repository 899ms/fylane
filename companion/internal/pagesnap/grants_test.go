package pagesnap

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// memStore is a grant store that keeps what it was given, and can fail.
type memStore struct {
	held    []Grant
	saves   int
	saveErr error
	loadErr error
}

func (m *memStore) Load() ([]Grant, error) { return m.held, m.loadErr }

func (m *memStore) Save(g []Grant) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saves++
	m.held = append([]Grant(nil), g...)
	return nil
}

func TestGrantsArePerWorkspaceAndWithdrawn(t *testing.T) {
	g := NewGrants()
	if g.Granted("ws_a") {
		t.Fatal("granted before being asked")
	}
	if err := g.Grant("ws_a"); err != nil {
		t.Fatal(err)
	}
	if err := g.Grant("ws_b"); err != nil {
		t.Fatal(err)
	}
	if !g.Granted("ws_a") || !g.Granted("ws_b") || g.Granted("ws_c") {
		t.Fatal("grant state wrong")
	}
	if list := g.List(); len(list) != 2 || list[0].WorkspaceID != "ws_a" {
		t.Fatalf("list %+v", list)
	}
	had, err := g.Revoke("ws_a")
	if err != nil || !had || g.Granted("ws_a") {
		t.Fatalf("revoke: %v %v", had, err)
	}
	if had, _ := g.Revoke("ws_a"); had {
		t.Fatal("revoking twice reports a grant twice")
	}

	s := &Service{Grants: g}
	if s.AskEveryTime() {
		t.Fatal("no rung asks every time")
	}
	s.Strict = func() bool { return true }
	if !s.AskEveryTime() {
		t.Fatal("the strict rung does not ask every time")
	}
}

// TestAGrantOutlivesTheProcess is the point of storing them: the user is
// told the folder will not be asked about again, and a restart must not
// make that untrue.
func TestAGrantOutlivesTheProcess(t *testing.T) {
	store := &memStore{}
	g, err := LoadGrants(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Grant("ws_a"); err != nil {
		t.Fatal(err)
	}
	if err := g.Grant("ws_a"); err != nil || store.saves != 1 {
		t.Fatalf("granting the same workspace twice wrote %d times: %v", store.saves, err)
	}

	again, err := LoadGrants(store)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Granted("ws_a") {
		t.Fatal("a stored grant did not survive a restart")
	}
	if list := again.List(); len(list) != 1 || list[0].GrantedAt.IsZero() {
		t.Fatalf("restored list %+v", list)
	}

	if _, err := again.Revoke("ws_a"); err != nil {
		t.Fatal(err)
	}
	third, err := LoadGrants(store)
	if err != nil {
		t.Fatal(err)
	}
	if third.Granted("ws_a") {
		t.Fatal("a withdrawn grant came back")
	}
}

// TestAGrantThatCannotBeWrittenIsNotClaimed: saying "this folder will not be
// asked about again" and then losing it is worse than failing the call.
func TestAGrantThatCannotBeWrittenIsNotClaimed(t *testing.T) {
	store := &memStore{saveErr: errors.New("disk is full")}
	g, err := LoadGrants(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Grant("ws_a"); err == nil || !strings.Contains(err.Error(), "disk is full") {
		t.Fatalf("grant error %v", err)
	}
	if g.Granted("ws_a") {
		t.Fatal("an unwritten grant is held anyway")
	}

	store.saveErr = nil
	if err := g.Grant("ws_a"); err != nil {
		t.Fatal(err)
	}
	store.saveErr = errors.New("disk is full")
	if had, err := g.Revoke("ws_a"); had || err == nil {
		t.Fatalf("revoke reported %v, %v", had, err)
	}
	if !g.Granted("ws_a") {
		t.Fatal("a revoke that could not be written was applied in memory")
	}
}

func TestLoadGrantsReportsAnUnreadableStore(t *testing.T) {
	if _, err := LoadGrants(&memStore{loadErr: errors.New("broken")}); err == nil {
		t.Fatal("an unreadable store loaded")
	}
}

func TestBrowserPathsCoverEachInstaller(t *testing.T) {
	env := map[string]string{"HOME": "/Users/u", "ProgramFiles": `C:\Program Files`, "LocalAppData": `C:\Users\u\AppData\Local`}
	get := func(k string) string { return env[k] }
	mac := strings.Join(browserPaths("darwin", get), "\n")
	for _, want := range []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Users/u/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"} {
		if !strings.Contains(mac, want) {
			t.Errorf("darwin paths lack %s", want)
		}
	}
	win := strings.Join(browserPaths("windows", get), "\n")
	for _, want := range []string{`C:\Program Files\Google\Chrome\Application\chrome.exe`, `C:\Users\u\AppData\Local\Microsoft\Edge\Application\msedge.exe`} {
		if !strings.Contains(win, want) {
			t.Errorf("windows paths lack %s", want)
		}
	}
	if strings.Contains(win, "(x86)") {
		t.Error("an unset ProgramFiles(x86) produced a path")
	}
	if len(browserPaths("linux", get)) != 0 {
		t.Error("linux uses PATH, not fixed paths")
	}
}

// Windows hands Go a clock coarse enough that two grants a few microseconds
// apart carry the identical instant — the CI failure that found this had both
// records at the same nanosecond, monotonic reading included. sort.Slice is
// not stable, so the order flipped between calls, and that order is what the
// settings page shows and what the store is handed.
//
// Loading from a store is how the same instant is reproduced here on any
// platform: LoadGrants keeps whatever timestamps it is given.
func TestGrantsListIsStableWhenTwoGrantsShareATimestamp(t *testing.T) {
	same := time.Date(2026, 9, 17, 5, 41, 21, 152553400, time.UTC)
	store := &memStore{held: []Grant{
		{WorkspaceID: "ws_b", GrantedAt: same},
		{WorkspaceID: "ws_a", GrantedAt: same},
	}}
	g, err := LoadGrants(store)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 20; i++ {
		list := g.List()
		if len(list) != 2 {
			t.Fatalf("list = %+v", list)
		}
		if list[0].WorkspaceID != "ws_a" || list[1].WorkspaceID != "ws_b" {
			t.Fatalf("call %d returned %s then %s; the order has to be the same every time",
				i, list[0].WorkspaceID, list[1].WorkspaceID)
		}
	}
}

// A later grant still sorts after an earlier one — the tiebreak must not
// have quietly become the only key.
func TestGrantsListStillPutsTheOlderGrantFirst(t *testing.T) {
	early := time.Date(2026, 9, 17, 5, 0, 0, 0, time.UTC)
	late := early.Add(time.Minute)
	store := &memStore{held: []Grant{
		{WorkspaceID: "ws_a", GrantedAt: late},
		{WorkspaceID: "ws_z", GrantedAt: early},
	}}
	g, err := LoadGrants(store)
	if err != nil {
		t.Fatal(err)
	}

	list := g.List()
	if list[0].WorkspaceID != "ws_z" {
		t.Fatalf("list = %+v; oldest first, whatever the ids say", list)
	}
}

// If the windows branch is ever built with filepath the same way darwin was,
// this catches it on a mac: filepath would answer with forward slashes there.
func TestWindowsBrowserPathsStayBackslashed(t *testing.T) {
	get := func(k string) string {
		return map[string]string{"ProgramFiles": `C:\Program Files`}[k]
	}
	for _, p := range browserPaths("windows", get) {
		if strings.Contains(p, "/") {
			t.Fatalf("windows path built with the host's separator: %s", p)
		}
	}
}
