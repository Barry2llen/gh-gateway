package upstream

import "testing"

func TestPinnedTransportNeverUsesEnvironmentProxy(t *testing.T) {
	transport := NewPinnedTransport("git.example.com", "10.0.0.20")
	if transport.Proxy != nil {
		t.Fatal("pinned transport retained an environment proxy")
	}
	if transport.TLSClientConfig == nil || transport.TLSClientConfig.ServerName != "git.example.com" || transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("TLS config = %+v", transport.TLSClientConfig)
	}
}
