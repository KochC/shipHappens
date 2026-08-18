// Package egress implements a filtering forward-proxy that enforces an egress
// allow-list. It supports plain HTTP (by Host header / request URL) and HTTPS
// via the CONNECT method (by target host). Any request to a host that is not on
// the allow-list is refused with 403, giving *real* network egress enforcement
// for container jobs (as opposed to advisory scoping).
package egress

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Proxy is a filtering forward-proxy.
type Proxy struct {
	allow   *Matcher
	dialer  *net.Dialer
	onBlock func(host string) // optional callback when a host is refused (for logging/tests)
}

// New builds a Proxy that only permits the given hosts (and their subdomains
// when an entry is written as "*.example.com"). An empty allow-list blocks all.
func New(allow []string) *Proxy {
	return &Proxy{
		allow:  NewMatcher(allow),
		dialer: &net.Dialer{Timeout: 10 * time.Second},
	}
}

// OnBlock registers a callback invoked with the host each time a request is
// refused. Useful for surfacing violations in logs.
func (p *Proxy) OnBlock(fn func(host string)) { p.onBlock = fn }

// ServeHTTP implements http.Handler: CONNECT tunnels for HTTPS, direct proxying
// for plain HTTP.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
		return
	}
	p.handleHTTP(w, r)
}

func (p *Proxy) refuse(w http.ResponseWriter, host string) {
	if p.onBlock != nil {
		p.onBlock(host)
	}
	http.Error(w, "egress blocked by Ship Happens allow-list: "+host, http.StatusForbidden)
}

func (p *Proxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	host := hostOnly(r.Host)
	if !p.allow.Allowed(host) {
		p.refuse(w, host)
		return
	}
	upstream, err := p.dialer.DialContext(r.Context(), "tcp", r.Host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		upstream.Close()
		http.Error(w, "hijacking unsupported", http.StatusInternalServerError)
		return
	}
	client, _, err := hj.Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	pipe(client, upstream)
}

func (p *Proxy) handleHTTP(w http.ResponseWriter, r *http.Request) {
	host := hostOnly(r.Host)
	if !p.allow.Allowed(host) {
		p.refuse(w, host)
		return
	}
	// Reconstruct an outbound request to the target.
	outURL := r.URL
	if !outURL.IsAbs() {
		outURL.Scheme = "http"
		outURL.Host = r.Host
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, outURL.String(), r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	copyHeader(req.Header, r.Header)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// transport is the outbound transport for plain-HTTP proxying (overridable in
// tests).
var transport http.RoundTripper = http.DefaultTransport

// pipe copies bidirectionally between two conns and closes both when done.
func pipe(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	cp := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		if c, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = c.CloseWrite()
		}
	}
	go cp(a, b)
	go cp(b, a)
	wg.Wait()
	a.Close()
	b.Close()
}

func copyHeader(dst, src http.Header) {
	for k, vs := range src {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

// hostOnly strips any :port suffix from a host[:port] string.
func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

// Serve runs the proxy on the given listener until the context is cancelled.
func (p *Proxy) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{Handler: p}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// ── allow-list matching ──────────────────────────────────────────────────────

// Matcher decides whether a host is permitted. Entries may be exact
// ("api.github.com") or wildcard ("*.github.com", which also matches the apex
// "github.com"). Matching is case-insensitive.
type Matcher struct {
	exact    map[string]struct{}
	suffixes []string // for "*.example.com" → ".example.com"; apex "example.com"
}

// NewMatcher compiles an allow-list.
func NewMatcher(allow []string) *Matcher {
	m := &Matcher{exact: map[string]struct{}{}}
	for _, a := range allow {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" {
			continue
		}
		if strings.HasPrefix(a, "*.") {
			apex := a[2:]
			m.suffixes = append(m.suffixes, "."+apex)
			m.exact[apex] = struct{}{} // wildcard also covers the apex
			continue
		}
		m.exact[a] = struct{}{}
	}
	return m
}

// Allowed reports whether host is permitted by the allow-list.
func (m *Matcher) Allowed(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	if _, ok := m.exact[host]; ok {
		return true
	}
	for _, suf := range m.suffixes {
		if strings.HasSuffix(host, suf) {
			return true
		}
	}
	return false
}

// String renders the allow-list for diagnostics.
func (m *Matcher) String() string {
	var parts []string
	for h := range m.exact {
		parts = append(parts, h)
	}
	return fmt.Sprintf("allow%v", parts)
}
