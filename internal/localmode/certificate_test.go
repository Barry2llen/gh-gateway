package localmode

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type memoryTrust struct {
	trusted                bool
	installedPath, removed string
}

func (m *memoryTrust) Trusted(string) (bool, error)   { return m.trusted, nil }
func (m *memoryTrust) Install(path string) error      { m.trusted, m.installedPath = true, path; return nil }
func (m *memoryTrust) Remove(thumbprint string) error { m.removed = thumbprint; return nil }

func TestCertificateManagerGeneratesTrustedHostCertificate(t *testing.T) {
	trust := &memoryTrust{}
	now := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	manager := fileCertificateManager{baseDir: t.TempDir(), trust: trust, now: func() time.Time { return now }}
	info, err := manager.Ensure("git.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Installed || info.Thumbprint == "" || trust.installedPath == "" {
		t.Fatalf("info = %+v trust = %+v", info, trust)
	}
	data, err := os.ReadFile(filepath.Join(info.CertDir, "git.example.com.crt"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatal("leaf PEM missing")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := leaf.VerifyHostname("git.example.com"); err != nil {
		t.Fatal(err)
	}
	if leaf.NotAfter.Sub(now) < 396*24*time.Hour {
		t.Fatalf("leaf validity too short: %s", leaf.NotAfter.Sub(now))
	}
	second, err := manager.Ensure("git.example.com")
	if err != nil {
		t.Fatalf("reuse existing certificates: %v", err)
	}
	if second.Thumbprint != info.Thumbprint {
		t.Fatalf("reused thumbprint = %q, want %q", second.Thumbprint, info.Thumbprint)
	}
}

func TestCertificateManagerRepairsEmptyLeafDirectory(t *testing.T) {
	dir := t.TempDir()
	trust := &memoryTrust{}
	manager := fileCertificateManager{baseDir: dir, trust: trust, now: time.Now}
	certDir := filepath.Join(dir, "certs")
	if err := os.MkdirAll(filepath.Join(certDir, "git.example.com.crt"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Ensure("git.example.com"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(certDir, "git.example.com.crt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.IsDir() {
		t.Fatal("leaf certificate path remained a directory")
	}
}
