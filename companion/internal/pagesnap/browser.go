package pagesnap

import (
	"os"
	osexec "os/exec"
	"path"
	"runtime"
)

// FindBrowser returns the path of a Chromium-family browser already installed
// on this machine, or "" when there is none. Nothing is ever downloaded: the
// runtime download boundary covers the tunnel binary and nothing else.
func FindBrowser() string {
	for _, c := range browserPaths(runtime.GOOS, os.Getenv) {
		if st, err := os.Stat(c); err == nil && st.Mode().IsRegular() {
			return c
		}
	}
	if runtime.GOOS == "linux" {
		for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge", "microsoft-edge-stable", "brave-browser"} {
			if p, err := osexec.LookPath(name); err == nil {
				return p
			}
		}
	}
	return ""
}

// browserPaths lists where each platform's installers put the browsers, in
// order of preference.
//
// The darwin branch joins with path, not filepath: it is naming paths on
// macOS, and filepath speaks whichever separator the machine running this
// happens to use. The two are the same on macOS and differ on Windows, so
// building a mac path with filepath produces backslashes on a Windows host —
// a function asked for one platform's paths answering in another's.
func browserPaths(goos string, getenv func(string) string) []string {
	var out []string
	switch goos {
	case "darwin":
		apps := []string{
			"Google Chrome.app/Contents/MacOS/Google Chrome",
			"Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"Chromium.app/Contents/MacOS/Chromium",
			"Brave Browser.app/Contents/MacOS/Brave Browser",
		}
		bases := []string{"/Applications"}
		if home := getenv("HOME"); home != "" {
			bases = append(bases, path.Join(home, "Applications"))
		}
		for _, base := range bases {
			for _, app := range apps {
				out = append(out, path.Join(base, app))
			}
		}
	case "windows":
		exes := []string{
			`Google\Chrome\Application\chrome.exe`,
			`Microsoft\Edge\Application\msedge.exe`,
			`Chromium\Application\chrome.exe`,
			`BraveSoftware\Brave-Browser\Application\brave.exe`,
		}
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
			base := getenv(env)
			if base == "" {
				continue
			}
			for _, exe := range exes {
				out = append(out, base+`\`+exe)
			}
		}
	}
	return out
}
