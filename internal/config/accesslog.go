package config

import "github.com/rs/zerolog"

// AccessLogCLI is the CLI configuration for the access log command.
type AccessLogCLI struct {
	GRPC           GRPCConfig    `embed:"" prefix:"grpc-" envprefix:"GRPC_"`
	Health         HealthConfig  `embed:"" prefix:"health-" envprefix:"HEALTH_"`
	LogLevel       zerolog.Level `name:"log-level" env:"LOG_LEVEL" default:"info" help:"Log level (debug, info, warn, error, fatal, panic)."`
	ExcludeHeaders []string      `name:"exclude-headers" env:"EXCLUDE_HEADERS" help:"Comma-separated list of headers to exclude from logging."`
}
