package subscription

import (
	"context"
	"net"
	"time"
)

// apexDialTimeout bounds one TCP attempt. The panel publishes six A records and
// on a normal day only one or two of them accept connections; the rest drop the
// SYN without an RST. That matters because net.Dialer walks the addresses in
// order: without a per-attempt deadline a single dead record eats the whole
// request budget, which is exactly how the panel used to look "down" for 20+
// seconds while a browser opened it instantly.
const apexDialTimeout = 4 * time.Second

// ApexDialContext resolves the host and races every address, returning the first
// connection that completes. Racing is what makes a half-dead round-robin
// harmless: the good record answers in ~40 ms and the dead ones are abandoned.
//
// It is exported so the config CDN (which has the same shape of DNS) can use it.
func ApexDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: apexDialTimeout, KeepAlive: 30 * time.Second}

	host, port, err := net.SplitHostPort(addr)
	if err != nil || net.ParseIP(host) != nil {
		// Not a host:port we can expand (or already an address): dial as asked.
		return dialer.DialContext(ctx, network, addr)
	}

	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(addrs) < 2 {
		return dialer.DialContext(ctx, network, addr)
	}

	raceCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		conn net.Conn
		err  error
	}
	results := make(chan result, len(addrs))
	for _, resolved := range addrs {
		go func(ip net.IP) {
			conn, dialErr := dialer.DialContext(raceCtx, network, net.JoinHostPort(ip.String(), port))
			results <- result{conn: conn, err: dialErr}
		}(resolved.IP)
	}

	remaining := len(addrs)
	var firstErr error
	for i := 0; i < len(addrs); i++ {
		got := <-results
		remaining--
		if got.err == nil {
			cancel()
			// The losers either fail because the race context is gone or complete
			// just after us; drain them off the hot path and close what still lands
			// so no connection leaks.
			go func(n int) {
				for j := 0; j < n; j++ {
					if late := <-results; late.conn != nil {
						late.conn.Close()
					}
				}
			}(remaining)
			return got.conn, nil
		}
		if firstErr == nil {
			firstErr = got.err
		}
	}
	return nil, firstErr
}
