package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadTLSConfig_NilConfig(t *testing.T) {
	cfg, err := LoadTLSConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Fatal("expected nil TLS config for nil input")
	}
}

func TestLoadTLSConfig_AllEmpty(t *testing.T) {
	cfg, err := LoadTLSConfig(&TLSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Fatal("expected nil TLS config for all-empty paths")
	}
}

func TestLoadTLSConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	caPath, certPath, keyPath := generateTestCerts(t, dir)

	cfg, err := LoadTLSConfig(&TLSConfig{
		CACert:   caPath,
		NodeCert: certPath,
		NodeKey:  keyPath,
	})
	if err != nil {
		t.Fatalf("LoadTLSConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil TLS config")
	}
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Errorf("MinVersion = %d, want TLS 1.3 (%d)", cfg.MinVersion, tls.VersionTLS13)
	}
	if len(cfg.Certificates) != 1 {
		t.Errorf("expected 1 certificate, got %d", len(cfg.Certificates))
	}
	if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Errorf("ClientAuth = %d, want RequireAndVerifyClientCert", cfg.ClientAuth)
	}
}

func TestLoadTLSConfig_BadCertPath(t *testing.T) {
	_, err := LoadTLSConfig(&TLSConfig{
		CACert:   "/nonexistent/ca.pem",
		NodeCert: "/nonexistent/cert.pem",
		NodeKey:  "/nonexistent/key.pem",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent cert paths")
	}
}

func TestLoadTLSConfig_BadCAPEM(t *testing.T) {
	dir := t.TempDir()
	_, certPath, keyPath := generateTestCerts(t, dir)

	// Write an invalid CA file
	badCA := filepath.Join(dir, "bad_ca.pem")
	if err := os.WriteFile(badCA, []byte("not a certificate"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadTLSConfig(&TLSConfig{
		CACert:   badCA,
		NodeCert: certPath,
		NodeKey:  keyPath,
	})
	if err == nil {
		t.Fatal("expected error for invalid CA PEM")
	}
}

// generateTestCerts creates a self-signed CA and a node certificate signed by
// that CA. It returns the file paths for the CA cert, node cert, and node key.
func generateTestCerts(t *testing.T, dir string) (caPath, certPath, keyPath string) {
	t.Helper()

	// Generate CA key and self-signed certificate
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{Organization: []string{"Test CA"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	caCertBytes, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}

	// Generate node key and certificate signed by CA
	nodeKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caCertBytes)
	if err != nil {
		t.Fatal(err)
	}
	nodeTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{Organization: []string{"Test Node"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}
	nodeCertBytes, err := x509.CreateCertificate(rand.Reader, nodeTemplate, caCert, &nodeKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}

	// Write CA cert PEM
	caPath = filepath.Join(dir, "ca.pem")
	caFile, err := os.Create(caPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(caFile, &pem.Block{Type: "CERTIFICATE", Bytes: caCertBytes}); err != nil {
		t.Fatal(err)
	}
	caFile.Close()

	// Write node cert PEM
	certPath = filepath.Join(dir, "node.pem")
	certFile, err := os.Create(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: nodeCertBytes}); err != nil {
		t.Fatal(err)
	}
	certFile.Close()

	// Write node key PEM
	keyPath = filepath.Join(dir, "node-key.pem")
	keyFile, err := os.Create(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	nodeKeyBytes, err := x509.MarshalECPrivateKey(nodeKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: nodeKeyBytes}); err != nil {
		t.Fatal(err)
	}
	keyFile.Close()

	return
}
