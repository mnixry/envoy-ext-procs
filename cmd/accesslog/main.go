package main

import (
	"os"

	"github.com/alecthomas/kong"
	"github.com/mnixry/envoy-ext-procs/internal/config"
	"github.com/mnixry/envoy-ext-procs/internal/extproc/accesslog"
	"github.com/mnixry/envoy-ext-procs/internal/logger"
	"github.com/mnixry/envoy-ext-procs/internal/server"
)

func main() {
	var cli config.AccessLogCLI
	kong.Parse(&cli,
		kong.Description("Envoy external processor that emits Caddy-style JSON access logs."),
		kong.UsageOnError(),
	)

	log := logger.New(cli.Log)

	log.Info().
		Strs("exclude_headers", cli.ExcludeHeaders).
		Str("log_output", cli.Log.Output).
		Str("log_format", string(cli.Log.Format)).
		Msg("access log processor configured")

	factory := accesslog.NewProcessorFactory(
		os.Stdout,
		log,
		accesslog.WithExcludeHeaders(cli.ExcludeHeaders...),
	)

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
