package pagesnap

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"
	"sync"
	"time"
)

// maxBlockedNames bounds the list of refused destinations a snapshot reports.
const maxBlockedNames = 20

// errBlocked is what the page gets for a destination the policy refuses.
var errBlocked = errors.New("blocked by page_snapshot")

// reachPolicy decides whether the page may connect to ip:port. It sees the
// address that is about to be dialled, after resolution, so a name that
// resolves to a loopback or private address is judged as that address.
type reachPolicy func(ip net.IP, port int) error

// proxy is the only way out of the snapshot browser. Chrome is started with
// it as its proxy for every scheme and with loopback taken off the bypass
// list, so plain requests, WebSockets and HTTPS tunnels all arrive here.
type proxy struct {
	ln     net.Listener
	srv    *http.Server
	allow  reachPolicy
	dialer net.Dialer

	mu      sync.Mutex
	blocked []string

	// stop is closed by close; tunnels still open end with it. stopped is
	// set under mu at the same moment, so no tunnel is counted into conns
	// after close has begun waiting on it.
	stop    chan struct{}
	stopped bool
	conns   sync.WaitGroup
}

func startProxy(allow reachPolicy) (*proxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &proxy{ln: ln, allow: allow, dialer: net.Dialer{Timeout: 10 * time.Second}, stop: make(chan struct{})}
	forward := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.Out.URL = r.In.URL
			r.Out.Host = r.In.Host
		},
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           p.dial,
			ResponseHeaderTimeout: 30 * time.Second,
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			if errors.Is(err, errBlocked) {
				http.Error(w, err.Error(), http.StatusForbidden)
				return
			}
			http.Error(w, "page_snapshot proxy: "+err.Error(), http.StatusBadGateway)
		},
	}
	p.srv = &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodConnect {
				p.tunnel(w, r)
				return
			}
			if r.URL.Host == "" {
				http.Error(w, "page_snapshot proxy: not a proxy request", http.StatusBadRequest)
				return
			}
			forward.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go p.srv.Serve(ln)
	return p, nil
}

func (p *proxy) addr() string { return p.ln.Addr().String() }

// close stops accepting and drops every connection still open, including
// tunnels a page left behind.
func (p *proxy) close() {
	p.mu.Lock()
	p.stopped = true
	close(p.stop)
	p.mu.Unlock()
	p.srv.Close()
	p.conns.Wait()
}

// track counts a tunnel in, unless the proxy is already closing.
func (p *proxy) track() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return false
	}
	p.conns.Add(1)
	return true
}

// names returns the destinations refused so far.
func (p *proxy) names() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.blocked...)
}

func (p *proxy) refuse(hostport string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, n := range p.blocked {
		if n == hostport {
			return
		}
	}
	if len(p.blocked) < maxBlockedNames {
		p.blocked = append(p.blocked, hostport)
	}
}

// dial resolves addr itself and connects only to an address the policy
// allows, trying each allowed address in turn. Resolving here rather than
// letting the dialer do it is what keeps a name from being judged as one
// address and connected to as another.
func (p *proxy) dial(ctx context.Context, _, addr string) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, a := range addrs {
			ips = append(ips, a.IP)
		}
	}
	var lastErr error
	allowed := false
	for _, ip := range ips {
		if p.allow(ip, port) != nil {
			continue
		}
		allowed = true
		conn, err := p.dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), portStr))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if !allowed {
		p.refuse(addr)
		return nil, errBlocked
	}
	return nil, lastErr
}

// tunnel serves CONNECT, which is how Chrome sends HTTPS and WebSockets
// through a proxy.
func (p *proxy) tunnel(w http.ResponseWriter, r *http.Request) {
	upstream, err := p.dial(r.Context(), "tcp", r.Host)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, errBlocked) {
			status = http.StatusForbidden
		}
		http.Error(w, err.Error(), status)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		upstream.Close()
		http.Error(w, "page_snapshot proxy: cannot tunnel", http.StatusInternalServerError)
		return
	}
	client, buf, err := hj.Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	if !p.track() {
		client.Close()
		upstream.Close()
		return
	}
	if _, err := client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		p.conns.Done()
		client.Close()
		upstream.Close()
		return
	}
	go func() {
		defer p.conns.Done()
		done := make(chan struct{}, 2)
		go func() {
			if buf.Reader.Buffered() > 0 {
				io.CopyN(upstream, buf, int64(buf.Reader.Buffered()))
			}
			io.Copy(upstream, client)
			done <- struct{}{}
		}()
		go func() {
			io.Copy(client, upstream)
			done <- struct{}{}
		}()
		select {
		case <-done:
		case <-p.stop:
		}
		client.Close()
		upstream.Close()
	}()
}
