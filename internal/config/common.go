package config

import "github.com/rs/zerolog"

// GRPCConfig holds gRPC server configuration.
type GRPCConfig struct {
	Port     int    `name:"port" env:"PORT" default:"9002" help:"gRPC server listen port."`
	CertPath string `name:"cert-path" env:"CERT_PATH" type:"path" required:"" help:"Path to directory containing server.crt and server.key for TLS."`
	CAFile   string `name:"ca-file" env:"CA_FILE" type:"path" help:"Path to CA certificate file for TLS."`
}

// HealthConfig holds health check server configuration.
type HealthConfig struct {
	Port           int    `name:"port" env:"PORT" default:"8080" help:"Health check HTTP server listen port."`
	DialServerName string `name:"dial-server-name" env:"DIAL_SERVER_NAME" default:"grpc-ext-proc.envoygateway" help:"TLS server name for health check gRPC dial."`
}

type LogFormat string

const (
	LogFormatJSON    LogFormat = "json"
	LogFormatConsole LogFormat = "console"
)

// LogConfig holds logging configuration.
type LogConfig struct {
	Level      zerolog.Level `name:"level" env:"LEVEL" default:"info" help:"Log level (trace, debug, info, warn, error, fatal, panic)."`
	Output     string        `name:"output" env:"OUTPUT" default:"stdout" help:"Log output location: 'stdout', 'stderr', or a file path."`
	Format     LogFormat     `name:"format" env:"FORMAT" default:"json" enum:"json,console" help:"Log format: 'json' or 'console'."`
	MaxSize    int           `name:"max-size" env:"MAX_SIZE" default:"100" help:"Max size in MB before log rotation (0 disables rotation)."`
	MaxAge     int           `name:"max-age" env:"MAX_AGE" default:"30" help:"Max age in days to retain old log files (0 keeps all)."`
	MaxBackups int           `name:"max-backups" env:"MAX_BACKUPS" default:"10" help:"Max number of old log files to retain (0 keeps all)."`
	Compress   bool          `name:"compress" env:"COMPRESS" default:"true" help:"Compress rotated log files with gzip."`
}
