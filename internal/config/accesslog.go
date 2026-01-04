package config

// AccessLogCLI is the CLI configuration for the access log command.
type AccessLogCLI struct {
	GRPC           GRPCConfig   `embed:"" prefix:"grpc-" envprefix:"GRPC_"`
	Health         HealthConfig `embed:"" prefix:"health-" envprefix:"HEALTH_"`
	Log            LogConfig    `embed:"" prefix:"log-" envprefix:"LOG_"`
	ExcludeHeaders []string     `name:"exclude-headers" env:"EXCLUDE_HEADERS" help:"Comma-separated list of headers to exclude from logging."`
}
