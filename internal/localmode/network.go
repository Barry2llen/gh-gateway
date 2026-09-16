package localmode

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gh-gateway/internal/upstream"
)

type resolver interface {
	Resolve(context.Context, string) (string, error)
}
type portChecker interface{ Available(int) error }
type prober interface {
	WaitLocal(context.Context, string) error
	LocalHTTPS(context.Context, string) error
	SystemHTTPS(context.Context, string, string) error
	GraphQL(context.Context, string) error
	Ordinary(context.Context, string) error
	SSH(context.Context, int) error
	UpstreamTLS(context.Context, string, string) error
}

type networkResolver struct{}

func (networkResolver) Resolve(ctx context.Context, host string) (string, error) {
	addresses, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil {
		return "", fmt.Errorf("resolve upstream %s: %w", host, err)
	}
	dialer := &net.Dialer{Timeout: 4 * time.Second}
	for _, address := range addresses {
		if address.IsLoopback() || address.IsUnspecified() {
			continue
		}
		connection, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(address.String(), "443"), &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host})
		if err == nil {
			_ = connection.Close()
			return address.String(), nil
		}
	}
	return "", fmt.Errorf("no resolved IPv4 address for %s passed upstream TLS validation", host)
}

type tcpPortChecker struct{}

func (tcpPortChecker) Available(port int) error {
	listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("127.0.0.1:%d is already in use", port)
	}
	return listener.Close()
}

type networkProber struct{}

func (networkProber) LocalHTTPS(ctx context.Context, host string) error {
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+"/api/v3/user", nil)
	client := &http.Client{Timeout: 3 * time.Second, Transport: upstream.NewPinnedTransport(host, "127.0.0.1")}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusUnauthorized && response.StatusCode != http.StatusForbidden {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return nil
}

func (networkProber) WaitLocal(ctx context.Context, host string) error {
	deadline := time.Now().Add(30 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		if err := (networkProber{}).LocalHTTPS(ctx, host); err == nil {
			return nil
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	return fmt.Errorf("local HTTPS health check failed: %w", last)
}

func (networkProber) SystemHTTPS(ctx context.Context, host, path string) error {
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+path, nil)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	response, err := (&http.Client{Timeout: 10 * time.Second, Transport: transport}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if path == "/api/v3/user" && (response.StatusCode == 200 || response.StatusCode == 401 || response.StatusCode == 403) {
		return nil
	}
	if response.StatusCode >= 500 {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return nil
}

func (networkProber) GraphQL(ctx context.Context, host string) error {
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+host+"/api/graphql", strings.NewReader(`{"query":"query { __typename }"}`))
	request.Header.Set("Content-Type", "application/json")
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	response, err := (&http.Client{Timeout: 10 * time.Second, Transport: transport}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound || response.StatusCode >= 500 {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return nil
}

func (p networkProber) Ordinary(ctx context.Context, host string) error {
	return p.SystemHTTPS(ctx, host, "/")
}

func (networkProber) SSH(ctx context.Context, port int) error {
	connection, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return err
	}
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	buffer := make([]byte, 4)
	if _, err := connection.Read(buffer); err != nil {
		return err
	}
	if string(buffer) != "SSH-" {
		return errors.New("upstream did not return an SSH banner")
	}
	return nil
}

func (networkProber) UpstreamTLS(ctx context.Context, host, ip string) error {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	connection, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(ip, "443"), &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host})
	if err != nil {
		return err
	}
	return connection.Close()
}
