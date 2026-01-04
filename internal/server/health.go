package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/mnixry/envoy-ext-procs/internal/tlsutil"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// HealthServer implements the gRPC Health Checking Protocol.
type HealthServer struct {
	grpc_health_v1.UnimplementedHealthServer
}

// Check implements the unary health check RPC.
func (s *HealthServer) Check(ctx context.Context, req *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{
		Status: grpc_health_v1.HealthCheckResponse_SERVING,
	}, nil
}

// Watch implements the streaming health check RPC.
func (s *HealthServer) Watch(req *grpc_health_v1.HealthCheckRequest, srv grpc_health_v1.Health_WatchServer) error {
	return srv.Send(&grpc_health_v1.HealthCheckResponse{
		Status: grpc_health_v1.HealthCheckResponse_SERVING,
	})
}

// HealthCheckHandler performs a health check by connecting to the local gRPC server
// and using the standard gRPC Health Checking Protocol.
func HealthCheckHandler(w http.ResponseWriter, r *http.Request, log zerolog.Logger, caFile string, grpcPort int, dialServerName string) {
	tlsConfig := &tls.Config{
		ServerName: dialServerName,
	}
	if certPool, err := tlsutil.LoadCA(caFile); err == nil {
		tlsConfig.RootCAs = certPool
	} else {
		log.Warn().Err(err).Msg("certificate verification disabled")
		tlsConfig.InsecureSkipVerify = true
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
	}

	conn, err := grpc.NewClient(fmt.Sprintf("localhost:%d", grpcPort), opts...)
	if err != nil {
		log.Warn().Err(err).Msg("health check failed")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	defer conn.Close()

	client := grpc_health_v1.NewHealthClient(conn)
	resp, err := client.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		log.Warn().Err(err).Msg("health check failed")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	if resp.GetStatus() == grpc_health_v1.HealthCheckResponse_SERVING {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusServiceUnavailable)
}
