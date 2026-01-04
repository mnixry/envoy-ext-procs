// Package server provides shared infrastructure for running ext_proc gRPC servers.
package server

import (
	"fmt"
	"net"
	"net/http"

	envoy_service_proc_v3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"github.com/mnixry/envoy-ext-procs/internal/extproc"
	"github.com/mnixry/envoy-ext-procs/internal/tlsutil"
	"github.com/rs/zerolog"
	"github.com/samber/oops"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// Config holds the common server configuration.
type Config struct {
	GRPCPort       int
	CertPath       string
	CAFile         string
	HealthPort     int
	DialServerName string
}

// Run starts the gRPC server and health check HTTP server.
// This function blocks until the health check server exits.
func Run(cfg Config, factory extproc.ProcessorFactory, log zerolog.Logger) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.GRPCPort))
	if err != nil {
		return oops.Wrapf(err, "failed to listen on port %d", cfg.GRPCPort)
	}

	certWatcher, err := tlsutil.NewCertWatcher(cfg.CertPath, log)
	if err != nil {
		return oops.Wrapf(err, "failed to create certificate watcher for %s", cfg.CertPath)
	}
	defer certWatcher.Close()

	server := extproc.NewServer(factory, log)
	gs := grpc.NewServer(grpc.Creds(certWatcher.TransportCredentials()))
	envoy_service_proc_v3.RegisterExternalProcessorServer(gs, server)
	grpc_health_v1.RegisterHealthServer(gs, &HealthServer{})

	log.Info().Int("port", cfg.GRPCPort).Msg("gRPC server listening")
	go func() {
		if err := gs.Serve(lis); err != nil {
			log.Fatal().Err(err).Msg("failed to serve gRPC")
		}
	}()

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		HealthCheckHandler(w, r, log, cfg.CAFile, cfg.GRPCPort, cfg.DialServerName)
	})
	log.Info().Int("port", cfg.HealthPort).Msg("health check server listening")
	if err := http.ListenAndServe(fmt.Sprintf(":%d", cfg.HealthPort), nil); err != nil {
		return oops.Wrapf(err, "failed to serve health check on port %d", cfg.HealthPort)
	}
	return nil
}
