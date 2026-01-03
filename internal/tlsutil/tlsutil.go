package tlsutil

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"

	"github.com/samber/oops"
	"google.golang.org/grpc/credentials"
)

func LoadTLSCredentials(certPath string) (credentials.TransportCredentials, error) {
	certFile := filepath.Join(certPath, "server.crt")
	keyFile := filepath.Join(certPath, "server.key")

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, oops.
			In("tlsutil").
			Code("LOAD_KEYPAIR_FAILED").
			With("cert_file", certFile).
			With("key_file", keyFile).
			Wrapf(err, "failed to load server key pair")
	}
	return credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}}), nil
}

func LoadCA(caPath string) (*x509.CertPool, error) {
	caFile := filepath.Join(caPath, "server.crt")

	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, oops.
			In("tlsutil").
			Code("READ_CA_FAILED").
			With("ca_file", caFile).
			Wrapf(err, "failed to read CA certificate")
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caCert)
	return pool, nil
}
