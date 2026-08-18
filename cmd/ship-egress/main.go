// Command ship-egress is a filtering forward-proxy that enforces an egress
// allow-list. Ship Happens runs it as a sidecar so container jobs with an
// `allow` list get real network egress enforcement: the job container is placed
// on an internal network with HTTP(S)_PROXY pointed here, and any request to a
// host not on the allow-list is refused.
//
// Usage:
//
//	ship-egress [-addr :8080] host1 host2 ...
//
// Hosts may also be supplied via the SHIP_ALLOW env var (comma-separated).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/chris/shiphappens/internal/egress"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	allow := flag.Args()
	if env := os.Getenv("SHIP_ALLOW"); env != "" {
		for _, h := range strings.Split(env, ",") {
			if h = strings.TrimSpace(h); h != "" {
				allow = append(allow, h)
			}
		}
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("ship-egress: listen: %v", err)
	}
	fmt.Fprintf(os.Stderr, "ship-egress: listening on %s, allow=%v\n", ln.Addr(), allow)

	p := egress.New(allow)
	p.OnBlock(func(host string) {
		fmt.Fprintf(os.Stderr, "ship-egress: BLOCKED %s\n", host)
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := p.Serve(ctx, ln); err != nil {
		log.Fatalf("ship-egress: serve: %v", err)
	}
}
