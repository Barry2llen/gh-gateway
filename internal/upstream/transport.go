package upstream

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"strings"
)

// NewPinnedTransport returns a transport that keeps HTTP Host and TLS SNI on
// host while connecting that host to a previously resolved IPv4 address.
func NewPinnedTransport(host, ip string) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Transparent mode must never delegate the pinned connection to an
	// environment-configured HTTP proxy, which would bypass the saved IP.
	transport.Proxy = nil
	dialer := &net.Dialer{}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		name, port, err := net.SplitHostPort(address)
		if err == nil && strings.EqualFold(strings.TrimSuffix(name, "."), strings.TrimSuffix(host, ".")) {
			address = net.JoinHostPort(ip, port)
		}
		return dialer.DialContext(ctx, network, address)
	}
	transport.TLSClientConfig = &tls.Config{ // #nosec G402 -- certificate verification remains enabled.
		MinVersion: tls.VersionTLS12,
		ServerName: host,
	}
	return transport
}
