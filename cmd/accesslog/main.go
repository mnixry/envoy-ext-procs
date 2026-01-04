package main

import (
	"io"
	"os"

	"github.com/alecthomas/kong"
	"github.com/mnixry/envoy-ext-procs/internal/config"
	"github.com/mnixry/envoy-ext-procs/internal/extproc/accesslog"
	"github.com/mnixry/envoy-ext-procs/internal/logger"
	"github.com/mnixry/envoy-ext-procs/internal/server"
	"github.com/rs/zerolog"
)

func main() {
	var cli config.AccessLogCLI
	kong.Parse(&cli,
		kong.Description("Envoy external processor that emits Caddy-style JSON access logs."),
		kong.UsageOnError(),
	)
	zerolog.SetGlobalLevel(cli.LogLevel)

	log := logger.New()

	// Setup access log output writer.
	var accessLogWriter io.Writer
	switch cli.AccessLog.Output {
	case "stdout":
		accessLogWriter = os.Stdout
	case "stderr":
		accessLogWriter = os.Stderr
	default:
		f, err := os.OpenFile(cli.AccessLog.Output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Fatal().
				Err(err).
				Str("output", cli.AccessLog.Output).
				Msg("failed to open access log file")
		}
		defer f.Close()
		accessLogWriter = f
	}

	log.Info().
		Str("output", cli.AccessLog.Output).
		Bool("include_request_headers", cli.AccessLog.IncludeRequestHeaders).
		Bool("include_response_headers", cli.AccessLog.IncludeResponseHeaders).
		Strs("exclude_headers", cli.AccessLog.ExcludeHeaders).
		Msg("access log processor configured")

	factory := accesslog.NewProcessorFactory(
		accessLogWriter,
		log,
		accesslog.WithRequestHeaders(cli.AccessLog.IncludeRequestHeaders),
		accesslog.WithResponseHeaders(cli.AccessLog.IncludeResponseHeaders),
		accesslog.WithExcludeHeaders(cli.AccessLog.ExcludeHeaders),
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
