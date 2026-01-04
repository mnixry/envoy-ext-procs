package main

import (
	"os"

	"github.com/alecthomas/kong"
	"github.com/mnixry/envoy-ext-procs/internal/config"
	"github.com/mnixry/envoy-ext-procs/internal/edgeone"
	edgeoneproc "github.com/mnixry/envoy-ext-procs/internal/extproc/edgeone"
	"github.com/mnixry/envoy-ext-procs/internal/logger"
	"github.com/mnixry/envoy-ext-procs/internal/server"
	"github.com/rs/zerolog"
)

func main() {
	var cli config.EdgeOneCLI
	kong.Parse(&cli,
		kong.Description("Envoy external processor that validates EdgeOne CDN requests and sets real client IP headers."),
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
		log.Fatal().Err(err).Msg("edgeone validator init failed")
	}

	log.Info().
		Str("api_endpoint", cli.EdgeOne.APIEndpoint).
		Str("region", cli.EdgeOne.Region).
		Int("cache_size", cli.EdgeOne.CacheSize).
		Dur("cache_ttl", cli.EdgeOne.CacheTTL).
		Dur("timeout", cli.EdgeOne.Timeout).
		Msg("edgeone validator configured")

	factory := edgeoneproc.NewProcessorFactory(validator, log)

	if err := server.Run(server.Config{
		GRPCPort:       cli.GRPC.Port,
		CertPath:       cli.GRPC.CertPath,
		CAFile:         cli.GRPC.CAFile,
		HealthPort:     cli.Health.Port,
		DialServerName: cli.Health.DialServerName,
	}, factory, log); err != nil {
		log.Fatal().Err(err).Send()
		os.Exit(1)
	}
}
