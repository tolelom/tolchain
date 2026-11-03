package config

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// LoadTLSConfig returns (nil, nil) when TLS is not configured. Peers must
// present a certificate from the same CA in both directions.
func LoadTLSConfig(cfg *TLSConfig) (*tls.Config, error) {
	if cfg == nil || (cfg.CACert == "" && cfg.NodeCert == "" && cfg.NodeKey == "") {
		return nil, nil
	}

	cert, err := tls.LoadX509KeyPair(cfg.NodeCert, cfg.NodeKey)
	if err != nil {
		return nil, fmt.Errorf("load node cert/key: %w", err)
	}

	caPEM, err := os.ReadFile(cfg.CACert)
	if err != nil {
		return nil, fmt.Errorf("read CA cert: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    caPool,
		RootCAs:      caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}, nil
}
