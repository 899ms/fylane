package approver

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	_ "embed"
)

// The page a paired device runs. It is one file with no framework, the way
// the authorization page is: a phone opens it from a QR code, keeps it as a
// home-screen app, and everything it needs arrives with the first request.
//
//go:embed page.html
var pageHTML string

//go:embed sw.js
var serviceWorker string

//go:embed manifest.webmanifest
var manifest string

//go:embed icon.png
var icon []byte

// The QR decoder the camera view runs (jsQR 1.4.0, Apache-2.0, vendored
// whole: the page must not load anything from a third-party host).
//
//go:embed third_party/jsQR.js
var qrDecoder []byte

// tagOf versions a file's address by its bytes. These files are cacheable
// for a day and the tunnel's edge honours that, so a replaced file must
// arrive under a new address or a phone keeps the old one.
func tagOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:4])
}

var iconTag, decoderTag = tagOf(icon), tagOf(qrDecoder)

// PageRoutes mounts the device page and its installable-app files. They are
// public: the page grants nothing by itself, and every call it makes is
// signed by a key only a paired device holds.
func (s *Service) PageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /approver", s.handlePage)
	mux.HandleFunc("GET /approver/", s.handlePage)
	mux.HandleFunc("GET /approver/sw.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Service-Worker-Allowed", "/approver/")
		w.Write([]byte(serviceWorker))
	})
	mux.HandleFunc("GET /approver/manifest.webmanifest", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write([]byte(strings.ReplaceAll(manifest, "{{icon}}", iconTag)))
	})
	mux.HandleFunc("GET /approver/icon.png", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(icon)
	})
	mux.HandleFunc("GET /approver/jsqr.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(qrDecoder)
	})
}

// handlePage serves the page under a policy that lets only its own inline
// script and style run, keyed by a nonce minted per response, plus the
// decoder served from this same origin. The page never embeds a prompt:
// everything it shows it fetched and opened itself.
func (s *Service) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/approver" && r.URL.Path != "/approver/" {
		http.NotFound(w, r)
		return
	}
	nonce := randomToken(16)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", strings.Join([]string{
		"default-src 'none'",
		"script-src 'self' 'nonce-" + nonce + "'",
		"style-src 'nonce-" + nonce + "'",
		"img-src 'self'",
		"connect-src 'self'",
		"manifest-src 'self'",
		"worker-src 'self'",
		"base-uri 'none'",
		"form-action 'none'",
	}, "; "))
	html := strings.NewReplacer("{{nonce}}", nonce, "{{icon}}", iconTag, "{{jsqr}}", decoderTag).Replace(pageHTML)
	w.Write([]byte(html))
}
