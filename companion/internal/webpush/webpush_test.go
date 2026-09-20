package webpush

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func b64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestTheEncryptionReproducesTheRFCsWorkedExample pins the construction to
// RFC 8291 Appendix A: same keys, same salt, same bytes out. A deviation in
// any HKDF label or in the framing would produce a message every browser
// silently drops, which no round trip through this package's own code can
// detect.
func TestTheEncryptionReproducesTheRFCsWorkedExample(t *testing.T) {
	sub := Subscription{
		Endpoint: "https://push.example.net/send/1",
		P256DH:   b64(t, "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"),
		Auth:     b64(t, "BTBZMqHH6r4Tts7J_aSIgg"),
	}
	asKey, err := ecdh.P256().NewPrivateKey(b64(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := encryptWith(sub, []byte("When I grow up, I want to be a watermelon"), asKey, b64(t, "DGv6ra1nlYgDCS1FRnbzlw"))
	if err != nil {
		t.Fatal(err)
	}
	want := b64(t, "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN")
	if !bytes.Equal(got, want) {
		t.Fatalf("message differs from the RFC's\n got %s\nwant %s", base64.RawURLEncoding.EncodeToString(got), base64.RawURLEncoding.EncodeToString(want))
	}
}

// browser is the receiving side, written from the RFC rather than from the
// sender, so a round trip means the two readings agree.
type browser struct {
	key  *ecdh.PrivateKey
	auth []byte
}

func newBrowser(t *testing.T) *browser {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	rand.Read(auth)
	return &browser{key: key, auth: auth}
}

func (b *browser) subscription(endpoint string) Subscription {
	return Subscription{Endpoint: endpoint, P256DH: b.key.PublicKey().Bytes(), Auth: b.auth}
}

func (b *browser) open(body []byte) ([]byte, error) {
	if len(body) < 16+4+1+65 {
		return nil, errors.New("message too short for a header")
	}
	salt, rs, idlen := body[:16], binary.BigEndian.Uint32(body[16:20]), int(body[20])
	if rs != 4096 || idlen != 65 {
		return nil, fmt.Errorf("header rs=%d idlen=%d", rs, idlen)
	}
	asPub, err := ecdh.P256().NewPublicKey(body[21 : 21+idlen])
	if err != nil {
		return nil, err
	}
	shared, err := b.key.ECDH(asPub)
	if err != nil {
		return nil, err
	}
	prkKey, _ := hkdf.Extract(sha256.New, shared, b.auth)
	info := append(append([]byte("WebPush: info\x00"), b.key.PublicKey().Bytes()...), asPub.Bytes()...)
	ikm, _ := hkdf.Expand(sha256.New, prkKey, string(info), 32)
	prk, _ := hkdf.Extract(sha256.New, ikm, salt)
	cek, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	record, err := gcm.Open(nil, nonce, body[21+idlen:], nil)
	if err != nil {
		return nil, fmt.Errorf("the browser could not open the message: %w", err)
	}
	if record[len(record)-1] != 0x02 {
		return nil, fmt.Errorf("last record must end in the 0x02 delimiter, got %x", record[len(record)-1])
	}
	return record[:len(record)-1], nil
}

// mustOpen is open for the cases where failing to is the failure.
func (b *browser) mustOpen(t *testing.T, body []byte) []byte {
	t.Helper()
	got, err := b.open(body)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestABrowserOpensWhatWasSealedForItAndNoOther(t *testing.T) {
	phone, other := newBrowser(t), newBrowser(t)
	body, err := Encrypt(phone.subscription("https://push.example.net/s/1"), []byte(`{"kind":"prompt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := phone.mustOpen(t, body); string(got) != `{"kind":"prompt"}` {
		t.Fatalf("opened %q", got)
	}
	again, _ := Encrypt(phone.subscription("https://push.example.net/s/1"), []byte(`{"kind":"prompt"}`))
	if bytes.Equal(body, again) {
		t.Fatal("two messages must not share a key or salt")
	}
	if _, err := other.open(body); err == nil {
		t.Fatal("another browser must not open the message")
	}
}

// loopbackOnly lets the tests reach an httptest server while the guard
// still runs on every dial.
func loopbackOnly(ip net.IP) error {
	if !ip.IsLoopback() {
		return errors.New("test policy: loopback only")
	}
	return nil
}

func testSender(t *testing.T, srv *httptest.Server) *Sender {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSender(key)
	if err != nil {
		t.Fatal(err)
	}
	s.checkIP = loopbackOnly
	if srv != nil {
		pool := x509.NewCertPool()
		pool.AddCert(srv.Certificate())
		s.tlsConfig = &tls.Config{RootCAs: pool}
	}
	s.now = func() time.Time { return time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC) }
	return s
}

// TestASendCarriesWhatThePushServiceChecks runs a push service that
// verifies the VAPID token against the key in the same header, and the
// content headers the encryption promises.
func TestASendCarriesWhatThePushServiceChecks(t *testing.T) {
	type seen struct {
		auth, enc, ttl, topic, urgency string
		body                           []byte
	}
	got := make(chan seen, 1)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- seen{r.Header.Get("Authorization"), r.Header.Get("Content-Encoding"), r.Header.Get("TTL"),
			r.Header.Get("Topic"), r.Header.Get("Urgency"), body}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	s := testSender(t, srv)
	phone := newBrowser(t)
	if err := s.Send(context.Background(), phone.subscription(srv.URL+"/send/abc"), []byte("x"), 60*time.Second, "fylane-approval"); err != nil {
		t.Fatal(err)
	}
	v := <-got
	if v.enc != "aes128gcm" || v.ttl != "60" || v.topic != "fylane-approval" || v.urgency != "high" {
		t.Errorf("headers %+v", v)
	}
	if string(phone.mustOpen(t, v.body)) != "x" {
		t.Error("the body is not the sealed message")
	}
	// Authorization: vapid t=<jwt>, k=<key>; the jwt verifies under k and
	// names this origin.
	if !strings.HasPrefix(v.auth, "vapid t=") {
		t.Fatalf("authorization %q", v.auth)
	}
	parts := strings.SplitN(strings.TrimPrefix(v.auth, "vapid t="), ", k=", 2)
	if len(parts) != 2 {
		t.Fatalf("authorization %q", v.auth)
	}
	token, k := parts[0], b64(t, parts[1])
	if !bytes.Equal(k, s.PublicKey()) || len(k) != 65 {
		t.Error("the key in the header is not the sender's")
	}
	segs := strings.Split(token, ".")
	if len(segs) != 3 {
		t.Fatalf("token %q", token)
	}
	var claims struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(b64(t, segs[1]), &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Aud != srv.URL || claims.Sub == "" || claims.Exp != s.now().Add(12*time.Hour).Unix() {
		t.Errorf("claims %+v (want aud %s)", claims, srv.URL)
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), k)
	pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}
	sig := b64(t, segs[2])
	digest := sha256.Sum256([]byte(segs[0] + "." + segs[1]))
	if len(sig) != 64 || !ecdsa.Verify(pub, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Error("the token does not verify under the header's key")
	}
}

func TestAGoneSubscriptionIsReportedAsSuch(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer srv.Close()
	s := testSender(t, srv)
	err := s.Send(context.Background(), newBrowser(t).subscription(srv.URL+"/send/x"), []byte("x"), time.Minute, "")
	if !errors.Is(err, ErrGone) {
		t.Fatalf("410 must surface as ErrGone, got %v", err)
	}
	fail := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer fail.Close()
	s = testSender(t, fail)
	err = s.Send(context.Background(), newBrowser(t).subscription(fail.URL+"/send/x"), []byte("x"), time.Minute, "")
	if err == nil || errors.Is(err, ErrGone) {
		t.Fatalf("a 429 is a failure, not a gone subscription: %v", err)
	}
}

// TestTheGuardHoldsOnTheWayOut: the endpoint is an address the phone chose,
// so it gets no more trust than any other outbound target — https only, no
// redirects, and the dial-time IP policy.
func TestTheGuardHoldsOnTheWayOut(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(201) }))
	defer plain.Close()
	s := testSender(t, nil)
	sub := newBrowser(t).subscription(plain.URL + "/send/x")
	if err := s.Send(context.Background(), sub, []byte("x"), time.Minute, ""); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("an http endpoint must be refused before any dial: %v", err)
	}

	hops := 0
	redirecting := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hops++
		http.Redirect(w, r, "https://push.example.net/elsewhere", http.StatusTemporaryRedirect)
	}))
	defer redirecting.Close()
	s = testSender(t, redirecting)
	err := s.Send(context.Background(), newBrowser(t).subscription(redirecting.URL+"/send/x"), []byte("x"), time.Minute, "")
	if err == nil || !strings.Contains(err.Error(), "redirect") || hops != 1 {
		t.Fatalf("a redirect must not be followed (hops %d): %v", hops, err)
	}

	// The production policy, with the loopback server: refused at dial time.
	s = testSender(t, redirecting)
	s.checkIP = nil
	err = s.Send(context.Background(), newBrowser(t).subscription(redirecting.URL+"/send/x"), []byte("x"), time.Minute, "")
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("the production policy must refuse a loopback push service: %v", err)
	}
}

func TestASubscriptionIsCheckedBeforeAnythingIsSent(t *testing.T) {
	good := newBrowser(t).subscription("https://push.example.net/s/1")
	bad := []Subscription{
		{Endpoint: "http://push.example.net/s/1", P256DH: good.P256DH, Auth: good.Auth},
		{Endpoint: "", P256DH: good.P256DH, Auth: good.Auth},
		{Endpoint: good.Endpoint, P256DH: good.P256DH[:64], Auth: good.Auth},
		{Endpoint: good.Endpoint, P256DH: good.P256DH, Auth: good.Auth[:15]},
	}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for i, b := range bad {
		if err := b.Validate(); err == nil {
			t.Errorf("subscription %d must be refused", i)
		}
	}
}
