package config

import "github.com/rs/zerolog"

// AccessLogCLI is the CLI configuration for the access log command.
type AccessLogCLI struct {
	GRPC      GRPCConfig      `embed:"" prefix:"grpc-" envprefix:"GRPC_"`
	Health    HealthConfig    `embed:"" prefix:"health-" envprefix:"HEALTH_"`
	AccessLog AccessLogConfig `embed:"" prefix:"accesslog-" envprefix:"ACCESSLOG_"`
	LogLevel  zerolog.Level   `name:"log-level" env:"LOG_LEVEL" default:"info" help:"Log level (debug, info, warn, error, fatal, panic)."`
}

// AccessLogConfig contains settings for access log output.
type AccessLogConfig struct {
	// Output specifies where to write access logs: "stdout", "stderr", or a file path.
	Output string `name:"output" env:"OUTPUT" default:"stdout" help:"Access log output: stdout, stderr, or file path."`
	// IncludeRequestHeaders enables logging of request headers.
	IncludeRequestHeaders bool `name:"include-request-headers" env:"INCLUDE_REQUEST_HEADERS" default:"true" help:"Include request headers in log entries."`
	// IncludeResponseHeaders enables logging of response headers.
	IncludeResponseHeaders bool `name:"include-response-headers" env:"INCLUDE_RESPONSE_HEADERS" default:"true" help:"Include response headers in log entries."`
	// ExcludeHeaders is a comma-separated list of header names to exclude from logging.
	ExcludeHeaders []string `name:"exclude-headers" env:"EXCLUDE_HEADERS" default:"authorization,cookie,set-cookie" help:"Comma-separated list of headers to exclude from logging."`
}
