package config

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
