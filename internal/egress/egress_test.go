package egress

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMatcher(t *testing.T) {
	m := NewMatcher([]string{"api.github.com", "*.example.com", "  ", ""})
	cases := map[string]bool{
		"api.github.com":      true,
		"API.GitHub.com":      true, // case-insensitive
		"github.com":          false,
		"example.com":         true, // apex covered by wildcard
		"sub.example.com":     true,
		"a.b.example.com":     true,
		"notexample.com":      false,
		"example.com.evil.io": false,
		"":                    false,
	}
	for host, want := range cases {
		if got := m.Allowed(host); got != want {
			t.Errorf("Allowed(%q)=%v want %v", host, got, want)
		}
	}
	if !strings.Contains(m.String(), "allow") {
		t.Error("String should render allow-list")
	}
}

func TestHostOnly(t *testing.T) {
	if hostOnly("a.b:443") != "a.b" || hostOnly("a.b") != "a.b" {
		t.Error("hostOnly")
	}
}

// startProxy spins up a proxy allowing `allow`, returns its client.
func startProxy(t *testing.T, allow []string) (*http.Client, *Proxy) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := New(allow)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go p.Serve(ctx, ln)
	proxyURL, _ := url.Parse("http://" + ln.Addr().String())
	client := &http.Client{
		Timeout:   3 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
	}
	// wait until the listener accepts
	for i := 0; i < 50; i++ {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err == nil {
			c.Close()
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	return client, p
}

func TestHTTPBlocked(t *testing.T) {
	var blocked string
	client, p := startProxy(t, []string{"allowed.test"})
	p.OnBlock(func(h string) { blocked = h })

	resp, err := client.Get("http://blocked.test/path")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("blocked host got %d, want 403", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "blocked.test") {
		t.Errorf("body should name the host: %s", body)
	}
	if blocked != "blocked.test" {
		t.Errorf("OnBlock host = %q", blocked)
	}
}

func TestHTTPAllowedForwards(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello from origin")
	}))
	defer origin.Close()
	host := hostOnly(origin.Listener.Addr().String())

	client, _ := startProxy(t, []string{host})
	resp, err := client.Get(origin.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello from origin" {
		t.Fatalf("allowed host not forwarded: %q (status %d)", body, resp.StatusCode)
	}
}

func TestConnectBlocked(t *testing.T) {
	client, _ := startProxy(t, []string{"allowed.test"})
	// HTTPS via CONNECT to a blocked host.
	_, err := client.Get("https://blocked.test/")
	if err == nil {
		t.Fatal("CONNECT to blocked host should fail")
	}
	if !strings.Contains(err.Error(), "Forbidden") && !strings.Contains(err.Error(), "403") {
		t.Errorf("expected 403-ish error, got %v", err)
	}
}

func TestConnectAllowedTunnels(t *testing.T) {
	// TLS origin.
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "secure")
	}))
	defer origin.Close()
	host := hostOnly(origin.Listener.Addr().String())

	client, _ := startProxy(t, []string{host})
	// Trust the test server's cert + use the proxy.
	tr := client.Transport.(*http.Transport).Clone()
	tr.TLSClientConfig = origin.Client().Transport.(*http.Transport).TLSClientConfig
	client.Transport = tr

	resp, err := client.Get(origin.URL + "/")
	if err != nil {
		t.Fatalf("CONNECT to allowed host failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "secure" {
		t.Errorf("tunnel body = %q", body)
	}
}

func TestEmptyAllowBlocksAll(t *testing.T) {
	client, _ := startProxy(t, nil)
	resp, err := client.Get("http://anything.test/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("empty allow-list should block all, got %d", resp.StatusCode)
	}
}

func TestServeShutsDownOnContext(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	p := New([]string{"x"})
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = p.Serve(ctx, ln) }()
	cancel()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after context cancel")
	}
}

func TestHTTPAllowedButUnreachable(t *testing.T) {
	client, _ := startProxy(t, []string{"localhost"})
	resp, err := client.Get("http://localhost:1/")
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		t.Fatal("allowed host should not be 403")
	}
}

func TestConnectAllowedButUnreachable(t *testing.T) {
	client, _ := startProxy(t, []string{"localhost"})
	_, err := client.Get("https://localhost:1/")
	if err == nil {
		t.Fatal("unreachable CONNECT should error")
	}
	if strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "Forbidden") {
		t.Errorf("allowed host wrongly filtered: %v", err)
	}
}

func TestHandleHTTPBadRequest(t *testing.T) {
	p := New([]string{"bad.test"})
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "http://bad.test/ok", nil)
	r.URL.Path = "\x7f"
	p.handleHTTP(rec, r)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("bad outbound request should be 502, got %d", rec.Code)
	}
}

func TestConnectHijackUnsupported(t *testing.T) {
	// httptest.ResponseRecorder does not implement http.Hijacker → 500.
	p := New([]string{"allowed.test"})
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodConnect, "//allowed.test:443", nil)
	r.Host = "allowed.test:443"
	// Force the allow-list to pass but dialing must be attempted; point at a
	// listener we control so Dial succeeds, then hijack fails on the recorder.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			c.Close()
		}
	}()
	r.Host = ln.Addr().String()
	p2 := New([]string{hostOnly(ln.Addr().String())})
	p2.handleConnect(rec, r)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("hijack-unsupported should be 500, got %d", rec.Code)
	}
	_ = p
}

func TestHandleHTTPAbsoluteURL(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "abs")
	}))
	defer origin.Close()
	host := hostOnly(origin.Listener.Addr().String())

	p := New([]string{host})
	rec := httptest.NewRecorder()
	// Absolute request URI (as a proxy client sends).
	r := httptest.NewRequest(http.MethodGet, origin.URL+"/", nil)
	p.handleHTTP(rec, r)
	if rec.Code != http.StatusOK || rec.Body.String() != "abs" {
		t.Errorf("absolute-URL proxying failed: %d %q", rec.Code, rec.Body.String())
	}
}

func TestServeListenerClosedError(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	ln.Close() // already closed → Serve returns an error (not ErrServerClosed)
	p := New([]string{"x"})
	if err := p.Serve(context.Background(), ln); err == nil {
		t.Error("Serve on a closed listener should error")
	}
}
