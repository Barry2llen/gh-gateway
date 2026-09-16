package localmode

import (
	"strings"
	"testing"
)

func TestDockerRunArgumentsArePinnedAndContainNoToken(t *testing.T) {
	args := dockerRunArguments(containerSpec{Name: "gh-gateway-a13f20", InstanceID: "id", Host: "git.example.com", UpstreamIP: "10.0.0.20", Image: "local:test", LeafCert: `C:\certs\leaf.crt`, LeafKey: `C:\certs\leaf.key`, CertVolume: "gh-gateway-a13f20-certs", SSHPort: 22, SSHProxy: true})
	joined := strings.Join(args, " ")
	for _, expected := range []string{"create --pull missing", "--read-only", "gh-gateway-a13f20-certs:/certs:ro", "127.0.0.1:443:443", "127.0.0.1:22:22", "GATEWAY_UPSTREAM_IP=10.0.0.20", "io.gh-gateway.managed=true", "local:test serve"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("arguments missing %q: %s", expected, joined)
		}
	}
	if strings.Contains(strings.ToUpper(joined), "TOKEN") || strings.Contains(strings.ToUpper(joined), "AUTHORIZATION") {
		t.Fatalf("credentials in arguments: %s", joined)
	}
}

func TestDockerCertificateLoaderPullsOnlyWhenMissing(t *testing.T) {
	args := dockerLoaderArguments(containerSpec{InstanceID: "id", Image: "local:test", CertVolume: "gh-gateway-a13f20-certs"}, "gh-gateway-a13f20-cert-loader")
	joined := strings.Join(args, " ")
	for _, expected := range []string{"create --pull missing", "--entrypoint /bin/sh", "gh-gateway-a13f20-certs:/certs", "local:test"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("arguments missing %q: %s", expected, joined)
		}
	}
}
