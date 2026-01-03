package config

import (
	"time"
)

type CLI struct {
	GRPC    GRPCConfig    `embed:"" prefix:"grpc-" envprefix:"GRPC_"`
	Health  HealthConfig  `embed:"" prefix:"health-" envprefix:"HEALTH_"`
	EdgeOne EdgeOneConfig `embed:"" prefix:"edgeone-" envprefix:"EDGEONE_"`
}

type GRPCConfig struct {
	Port     int    `name:"port" env:"PORT" default:"9002" help:"gRPC server listen port."`
	CertPath string `name:"cert-path" env:"CERT_PATH" type:"path" required:"" help:"Path to directory containing server.crt and server.key for TLS."`
}

type HealthConfig struct {
	Port           int    `name:"port" env:"PORT" default:"8080" help:"Health check HTTP server listen port."`
	DialServerName string `name:"dial-server-name" env:"DIAL_SERVER_NAME" default:"grpc-ext-proc.envoygateway" help:"TLS server name for health check gRPC dial."`
}

type EdgeOneConfig struct {
	SecretID    string        `name:"secret-id" env:"SECRET_ID" required:"" help:"Tencent Cloud SecretId for TEO API."`
	SecretKey   string        `name:"secret-key" env:"SECRET_KEY" required:"" help:"Tencent Cloud SecretKey for TEO API."`
	APIEndpoint string        `name:"api-endpoint" env:"API_ENDPOINT" default:"teo.tencentcloudapi.com" help:"Tencent EdgeOne TEO API endpoint (hostname or URL)."`
	Region      string        `name:"region" env:"REGION" default:"" help:"Tencent Cloud region for TEO client (optional)."`
	CacheSize   int           `name:"cache-size" env:"CACHE_SIZE" default:"1000" help:"LRU cache size for IP validation results."`
	CacheTTL    time.Duration `name:"cache-ttl" env:"CACHE_TTL" default:"1h" help:"Cache TTL for IP validation results (e.g. 1h, 30m)."`
	Timeout     time.Duration `name:"timeout" env:"TIMEOUT" default:"5s" help:"Tencent API request timeout (e.g. 5s, 10s)."`
}
