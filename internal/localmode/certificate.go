package localmode

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- Windows certificate thumbprints use SHA-1 identifiers.
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type certInfo struct {
	Thumbprint, CertDir, LeafCert, LeafKey string
	Installed                              bool
}

type certificateManager interface {
	Ensure(host string) (certInfo, error)
	Trusted(thumbprint string) (bool, error)
	RemoveTrust(thumbprint string) error
}

type trustStore interface {
	Trusted(string) (bool, error)
	Install(string) error
	Remove(string) error
}
type fileCertificateManager struct {
	baseDir  string
	trust    trustStore
	restrict func(string) error
	now      func() time.Time
}

func (m fileCertificateManager) Ensure(host string) (certInfo, error) {
	if m.now == nil {
		m.now = time.Now
	}
	certDir := filepath.Join(m.baseDir, "certs")
	if err := os.MkdirAll(certDir, 0o700); err != nil {
		return certInfo{}, err
	}
	caCertPath, caKeyPath := filepath.Join(certDir, "ca.crt"), filepath.Join(certDir, "ca.key")
	caCert, caKey, err := loadCA(caCertPath, caKeyPath)
	if errors.Is(err, os.ErrNotExist) {
		caCert, caKey, err = generateCA(m.now())
		if err == nil {
			err = writeCertificateAndKey(caCertPath, caKeyPath, caCert.Raw, caKey, m.restrict)
		}
	}
	if err != nil {
		return certInfo{}, fmt.Errorf("prepare local CA: %w", err)
	}
	thumbprint := certificateThumbprint(caCert.Raw)
	trusted, err := m.trust.Trusted(thumbprint)
	if err != nil {
		return certInfo{}, err
	}
	if !trusted {
		if err := m.trust.Install(caCertPath); err != nil {
			return certInfo{}, fmt.Errorf("install local CA: %w", err)
		}
		trusted, err = m.trust.Trusted(thumbprint)
		if err != nil {
			return certInfo{}, err
		}
		if !trusted {
			return certInfo{}, errors.New("local CA installation did not appear in the Current User Root store")
		}
	}
	leafCertPath, leafKeyPath := filepath.Join(certDir, host+".crt"), filepath.Join(certDir, host+".key")
	if err := repairEmptyDirectory(leafCertPath, "leaf certificate"); err != nil {
		return certInfo{}, err
	}
	if err := repairEmptyDirectory(leafKeyPath, "leaf private key"); err != nil {
		return certInfo{}, err
	}
	if !validLeaf(leafCertPath, host, m.now().Add(30*24*time.Hour)) {
		leaf, key, err := generateLeaf(host, caCert, caKey, m.now())
		if err != nil {
			return certInfo{}, err
		}
		if err := writeCertificateAndKey(leafCertPath, leafKeyPath, leaf, key, m.restrict); err != nil {
			return certInfo{}, fmt.Errorf("write leaf certificate and key: %w", err)
		}
	}
	return certInfo{Thumbprint: thumbprint, CertDir: certDir, LeafCert: leafCertPath, LeafKey: leafKeyPath, Installed: trusted}, nil
}

func repairEmptyDirectory(path, description string) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect %s path: %w", description, err)
	}
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return fmt.Errorf("inspect unexpected %s directory: %w", description, err)
		}
		if len(entries) != 0 {
			return fmt.Errorf("%s path points to a non-empty directory; refusing cleanup: %s", description, path)
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove empty %s directory: %w", description, err)
		}
	}
	return nil
}

func (m fileCertificateManager) Trusted(thumbprint string) (bool, error) {
	return m.trust.Trusted(thumbprint)
}
func (m fileCertificateManager) RemoveTrust(thumbprint string) error {
	trusted, err := m.trust.Trusted(thumbprint)
	if err != nil {
		return err
	}
	if !trusted {
		return nil
	}
	return m.trust.Remove(thumbprint)
}

func generateCA(now time.Time) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "gh-gateway Local CA", Organization: []string{"gh-gateway"}}, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(raw)
	return cert, key, err
}

func generateLeaf(host string, ca *x509.Certificate, caKey *ecdsa.PrivateKey, now time.Time) ([]byte, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: host}, DNSNames: []string{host}, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.Add(397 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	raw, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	return raw, key, err
}

func writeCertificateAndKey(certPath, keyPath string, certDER []byte, key *ecdsa.PrivateKey, restrict func(string) error) error {
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return err
	}
	if restrict != nil {
		return restrict(keyPath)
	}
	return nil
}

func loadCA(certPath, keyPath string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, err
	}
	certBlock, _ := pem.Decode(certPEM)
	keyBlock, _ := pem.Decode(keyPEM)
	if certBlock == nil || keyBlock == nil {
		return nil, nil, errors.New("invalid CA PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}
	parsed, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, nil, errors.New("CA key is not ECDSA")
	}
	return cert, key, nil
}

func validLeaf(path, host string, validAfter time.Time) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}
	return cert.VerifyHostname(host) == nil && cert.NotAfter.After(validAfter)
}

func certificateThumbprint(raw []byte) string {
	sum := sha1.Sum(raw)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}
