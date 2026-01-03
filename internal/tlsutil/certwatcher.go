package tlsutil

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/samber/oops"
	"google.golang.org/grpc/credentials"
)

// CertWatcher watches TLS certificate files and reloads them when modified.
// It checks file mtime on each GetCertificate call—simple and reliable.
type CertWatcher struct {
	certFile string
	keyFile  string
	log      zerolog.Logger

	mu      sync.RWMutex
	cert    *tls.Certificate
	modTime time.Time
}

// NewCertWatcher creates a new certificate watcher for the given cert directory.
func NewCertWatcher(certPath string, log zerolog.Logger) (*CertWatcher, error) {
	cw := &CertWatcher{
		certFile: filepath.Join(certPath, "server.crt"),
		keyFile:  filepath.Join(certPath, "server.key"),
		log:      log.With().Str("component", "cert_watcher").Logger(),
	}

	if err := cw.reload(); err != nil {
		return nil, err
	}

	cw.log.Info().
		Str("cert_file", cw.certFile).
		Str("key_file", cw.keyFile).
		Msg("certificate watcher initialized")

	return cw, nil
}

// latestModTime returns the most recent mtime of cert or key file.
func (cw *CertWatcher) latestModTime() (time.Time, error) {
	certInfo, err := os.Stat(cw.certFile)
	if err != nil {
		return time.Time{}, err
	}
	keyInfo, err := os.Stat(cw.keyFile)
	if err != nil {
		return time.Time{}, err
	}

	certMod := certInfo.ModTime()
	keyMod := keyInfo.ModTime()
	if keyMod.After(certMod) {
		return keyMod, nil
	}
	return certMod, nil
}

// reload loads the certificate from disk.
func (cw *CertWatcher) reload() error {
	cert, err := tls.LoadX509KeyPair(cw.certFile, cw.keyFile)
	if err != nil {
		return oops.
			In("tlsutil").
			Code("LOAD_KEYPAIR_FAILED").
			With("cert_file", cw.certFile).
			With("key_file", cw.keyFile).
			Wrapf(err, "failed to load server key pair")
	}

	modTime, err := cw.latestModTime()
	if err != nil {
		return oops.
			In("tlsutil").
			Code("STAT_FAILED").
			Wrapf(err, "failed to stat certificate files")
	}

	cw.mu.Lock()
	cw.cert = &cert
	cw.modTime = modTime
	cw.mu.Unlock()

	cw.log.Info().
		Str("cert_file", cw.certFile).
		Str("key_file", cw.keyFile).
		Time("mod_time", modTime).
		Msg("certificate loaded")

	return nil
}

// maybeReload checks if certificate files have changed and reloads if needed.
func (cw *CertWatcher) maybeReload() {
	modTime, err := cw.latestModTime()
	if err != nil {
		cw.log.Warn().Err(err).Msg("failed to stat certificate files")
		return
	}

	cw.mu.RLock()
	needsReload := modTime.After(cw.modTime)
	cw.mu.RUnlock()

	if needsReload {
		cw.log.Debug().
			Time("old_mod_time", cw.modTime).
			Time("new_mod_time", modTime).
			Msg("certificate file changed, reloading")

		if err := cw.reload(); err != nil {
			cw.log.Error().Err(err).Msg("failed to reload certificate, keeping previous")
		}
	}
}

// GetCertificate returns the current certificate. Checks for updates on each call.
// Suitable for use with tls.Config.GetCertificate.
func (cw *CertWatcher) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	cw.maybeReload()

	cw.mu.RLock()
	defer cw.mu.RUnlock()

	cw.log.Debug().
		Str("server_name", hello.ServerName).
		Msg("serving certificate")

	return cw.cert, nil
}

// Close is a no-op but kept for interface compatibility.
func (cw *CertWatcher) Close() error {
	return nil
}

// TransportCredentials returns gRPC transport credentials using the watched certificate.
func (cw *CertWatcher) TransportCredentials() credentials.TransportCredentials {
	return credentials.NewTLS(&tls.Config{
		GetCertificate: cw.GetCertificate,
	})
}
