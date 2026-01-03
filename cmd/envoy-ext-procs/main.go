package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/alecthomas/kong"
	envoy_service_proc_v3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"github.com/mnixry/envoy-ext-procs/internal/config"
	"github.com/mnixry/envoy-ext-procs/internal/edgeone"
	"github.com/mnixry/envoy-ext-procs/internal/extproc"
	"github.com/mnixry/envoy-ext-procs/internal/logger"
	"github.com/mnixry/envoy-ext-procs/internal/tlsutil"
	"github.com/rs/zerolog"
	"github.com/samber/oops"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func main() {
	var cli config.CLI
	kong.Parse(&cli,
		kong.Description("Envoy external processor gRPC server with EdgeOne IP validation."),
		kong.UsageOnError(),
	)
	zerolog.SetGlobalLevel(cli.LogLevel)

	log := logger.New()
	validator, err := edgeone.New(edgeone.Config{
		SecretID:    cli.EdgeOne.SecretID,
		SecretKey:   cli.EdgeOne.SecretKey,
		APIEndpoint: cli.EdgeOne.APIEndpoint,
		Region:      cli.EdgeOne.Region,
		CacheSize:   cli.EdgeOne.CacheSize,
		CacheTTL:    cli.EdgeOne.CacheTTL,
		Timeout:     cli.EdgeOne.Timeout,
	}, log)
	if err != nil {
		log.Fatal().Err(oops.Wrapf(err, "edgeone validator init failed")).Send()
	}
	log.Info().
		Str("api_endpoint", cli.EdgeOne.APIEndpoint).
		Str("region", cli.EdgeOne.Region).
		Int("cache_size", cli.EdgeOne.CacheSize).
		Dur("cache_ttl", cli.EdgeOne.CacheTTL).
		Dur("timeout", cli.EdgeOne.Timeout).
		Msg("edgeone enabled")

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", cli.GRPC.Port))
	if err != nil {
		log.Fatal().Err(oops.Wrapf(err, "failed to listen on port %d", cli.GRPC.Port)).Send()
	}

	certWatcher, err := tlsutil.NewCertWatcher(cli.GRPC.CertPath, log)
	if err != nil {
		log.Fatal().Err(oops.Wrapf(err, "failed to create certificate watcher for %s", cli.GRPC.CertPath)).Send()
	}
	defer certWatcher.Close()

	gs := grpc.NewServer(grpc.Creds(certWatcher.TransportCredentials()))
	envoy_service_proc_v3.RegisterExternalProcessorServer(gs, extproc.New(validator, log))

	log.Info().Int("port", cli.GRPC.Port).Msg("gRPC server listening")
	go func() {
		if err := gs.Serve(lis); err != nil {
			log.Fatal().Err(oops.Wrapf(err, "failed to serve gRPC")).Send()
		}
	}()

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		healthCheckHandler(w, r, log, cli.GRPC.CAFile, cli.GRPC.Port, cli.Health.DialServerName)
	})
	log.Info().Int("port", cli.Health.Port).Msg("health check server listening")
	if err := http.ListenAndServe(fmt.Sprintf(":%d", cli.Health.Port), nil); err != nil {
		log.Fatal().Err(oops.Wrapf(err, "failed to serve health check on port %d", cli.Health.Port)).Send()
		os.Exit(1)
	}
}

func healthCheckHandler(w http.ResponseWriter, r *http.Request, log zerolog.Logger, caFile string, grpcPort int, dialServerName string) {
	tlsConfig := &tls.Config{
		ServerName: dialServerName,
	}
	if certPool, err := tlsutil.LoadCA(caFile); err == nil {
		tlsConfig.RootCAs = certPool
	} else {
		log.Warn().Err(oops.Wrapf(err, "could not load CA certificate")).Msg("certificate verification disabled")
		tlsConfig.InsecureSkipVerify = true
	}

	// Create gRPC dial options.
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
	}

	conn, err := grpc.NewClient(fmt.Sprintf("localhost:%d", grpcPort), opts...)
	if err != nil {
		log.Warn().Err(oops.Wrapf(err, "could not connect to gRPC server")).Msg("healthz failed")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	defer conn.Close()

	client := envoy_service_proc_v3.NewExternalProcessorClient(conn)
	processor, err := client.Process(context.Background())
	if err != nil {
		log.Warn().Err(oops.Wrapf(err, "could not open ext_proc stream")).Msg("healthz failed")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	err = processor.Send(&envoy_service_proc_v3.ProcessingRequest{
		Request: &envoy_service_proc_v3.ProcessingRequest_RequestHeaders{
			RequestHeaders: &envoy_service_proc_v3.HttpHeaders{},
		},
	})
	if err != nil {
		log.Warn().Err(oops.Wrapf(err, "could not send request")).Msg("healthz failed")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	response, err := processor.Recv()
	if err != nil {
		log.Warn().Err(oops.Wrapf(err, "could not receive response")).Msg("healthz failed")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	if response != nil && response.GetRequestHeaders() != nil && response.GetRequestHeaders().GetResponse().GetStatus() == envoy_service_proc_v3.CommonResponse_CONTINUE {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusServiceUnavailable)
}
