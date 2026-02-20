// Package certgen generates a self-signed CA and node certificate/key
// pairs suitable for mTLS between TOL Chain nodes.
package certgen

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Options configures additional Subject Alternative Names for the node cert.
type Options struct {
	ExtraIPs []net.IP // additional IP SANs (e.g. external IP)
	ExtraDNS []string // additional DNS SANs (e.g. hostname)
}

// GenerateAll creates a CA certificate and a node certificate signed by that
// CA, writing four PEM files into dir:
//
//	ca.crt, ca.key, <nodeID>.crt, <nodeID>.key
//
// All files are created with 0600 permissions.
// Pass nil opts for localhost-only defaults.
func GenerateAll(dir, nodeID string, opts *Options) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	// ---- CA key + cert ----
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate CA key: %w", err)
	}

	caSerial, err := randomSerial()
	if err != nil {
		return err
	}

	caTemplate := &x509.Certificate{
		SerialNumber: caSerial,
		Subject:      pkix.Name{CommonName: "TOL Chain CA"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour), // ~10 years
		KeyUsage:     x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		IsCA:         true,
		BasicConstraintsValid: true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create CA cert: %w", err)
	}
	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		return fmt.Errorf("parse CA cert: %w", err)
	}

	if err := writePEM(filepath.Join(dir, "ca.crt"), "CERTIFICATE", caCertDER); err != nil {
		return err
	}
	caKeyDER, err := x509.MarshalECPrivateKey(caKey)
	if err != nil {
		return err
	}
	if err := writePEM(filepath.Join(dir, "ca.key"), "EC PRIVATE KEY", caKeyDER); err != nil {
		return err
	}

	// ---- Node key + cert ----
	nodeKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate node key: %w", err)
	}

	nodeSerial, err := randomSerial()
	if err != nil {
		return err
	}

	ips := []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}
	dns := []string{"localhost", nodeID}
	if opts != nil {
		ips = append(ips, opts.ExtraIPs...)
		dns = append(dns, opts.ExtraDNS...)
	}

	nodeTemplate := &x509.Certificate{
		SerialNumber: nodeSerial,
		Subject:      pkix.Name{CommonName: nodeID},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(5 * 365 * 24 * time.Hour), // ~5 years
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		IPAddresses:  ips,
		DNSNames:     dns,
	}

	nodeCertDER, err := x509.CreateCertificate(rand.Reader, nodeTemplate, caCert, &nodeKey.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create node cert: %w", err)
	}

	if err := writePEM(filepath.Join(dir, nodeID+".crt"), "CERTIFICATE", nodeCertDER); err != nil {
		return err
	}
	nodeKeyDER, err := x509.MarshalECPrivateKey(nodeKey)
	if err != nil {
		return err
	}
	if err := writePEM(filepath.Join(dir, nodeID+".key"), "EC PRIVATE KEY", nodeKeyDER); err != nil {
		return err
	}

	return nil
}

func randomSerial() (*big.Int, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}
	return serial, nil
}

func writePEM(path, typ string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: typ, Bytes: data})
}
