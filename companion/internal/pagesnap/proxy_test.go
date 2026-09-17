package pagesnap

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func portOf(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	u, _ := url.Parse(srv.URL)
	p, _ := strconv.Atoi(u.Port())
	return p
}

func countingServer(t *testing.T, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// TestTheProxyLetsThePageReachOnlyWhatThePolicyAllows covers both ways a
// browser speaks to a proxy — absolute-URL requests and CONNECT tunnels —
// because a guard on one of them is no guard.
func TestTheProxyLetsThePageReachOnlyWhatThePolicyAllows(t *testing.T) {
	mine, mineHits := countingServer(t, "mine")
	other, otherHits := countingServer(t, "other")
	minePort, otherPort := portOf(t, mine), portOf(t, other)

	px, err := startProxy(func(ip net.IP, port int) error {
		if ip.IsLoopback() && port == minePort {
			return nil
		}
		return errBlocked
	})
	if err != nil {
		t.Fatal(err)
	}
	defer px.close()
	proxyURL, _ := url.Parse("http://" + px.addr())
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 5 * time.Second}

	get := func(target string) (int, string) {
		resp, err := client.Get(target)
		if err != nil {
			t.Fatalf("GET %s: %v", target, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	if code, body := get(fmt.Sprintf("http://localhost:%d/", minePort)); code != 200 || body != "mine" {
		t.Fatalf("own port by name: %d %q", code, body)
	}
	if code, _ := get(fmt.Sprintf("http://127.0.0.1:%d/", otherPort)); code != http.StatusForbidden {
		t.Fatalf("other loopback port: %d", code)
	}
	if code, _ := get("http://10.255.255.1:80/"); code != http.StatusForbidden {
		t.Fatalf("private address: %d", code)
	}

	tunnel := func(target string) (string, net.Conn) {
		conn, err := net.DialTimeout("tcp", px.addr(), 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
		status, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			t.Fatalf("CONNECT %s: %v", target, err)
		}
		return status, conn
	}
	status, conn := tunnel(fmt.Sprintf("127.0.0.1:%d", otherPort))
	conn.Close()
	if !strings.Contains(status, "403") {
		t.Fatalf("CONNECT to another port: %q", status)
	}
	status, conn = tunnel(fmt.Sprintf("127.0.0.1:%d", minePort))
	if !strings.Contains(status, "200") {
		t.Fatalf("CONNECT to the own port: %q", status)
	}
	fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reply, _ := io.ReadAll(conn)
	conn.Close()
	if !strings.HasSuffix(string(reply), "mine") {
		t.Fatalf("through the tunnel: %q", reply)
	}

	if otherHits.Load() != 0 {
		t.Fatalf("the other server was reached %d times", otherHits.Load())
	}
	if mineHits.Load() != 2 {
		t.Fatalf("own server hits = %d, want 2", mineHits.Load())
	}
	names := px.names()
	for _, want := range []string{fmt.Sprintf("127.0.0.1:%d", otherPort), "10.255.255.1:80"} {
		found := false
		for _, n := range names {
			found = found || n == want
		}
		if !found {
			t.Errorf("blocked names %v lack %s", names, want)
		}
	}
}

func TestTheProxyClosesTunnelsAPageLeftOpen(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go io.Copy(io.Discard, c)
		}
	}()
	px, err := startProxy(func(net.IP, int) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", px.addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: x\r\n\r\n", ln.Addr())
	if status, _ := bufio.NewReader(conn).ReadString('\n'); !strings.Contains(status, "200") {
		t.Fatalf("CONNECT: %q", status)
	}

	closed := make(chan struct{})
	go func() {
		px.close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("close waited on a tunnel the page never ended")
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Read(make([]byte, 1)); err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("tunnel still open after close: %v", err)
	}
}
