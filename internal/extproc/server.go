package extproc

import (
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"time"

	envoy_api_v3_core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoy_service_proc_v3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"github.com/rs/zerolog"
	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements the Envoy ExternalProcessor gRPC service.
// It delegates request processing to a ProcessorFactory.
type Server struct {
	envoy_service_proc_v3.UnimplementedExternalProcessorServer

	factory ProcessorFactory
	log     zerolog.Logger
}

// NewServer creates a new ext_proc Server with the given ProcessorFactory.
func NewServer(factory ProcessorFactory, log zerolog.Logger) *Server {
	return &Server{
		factory: factory,
		log:     log.With().Str("component", "extproc").Logger(),
	}
}

// Process handles the bidirectional streaming RPC for external processing.
func (s *Server) Process(srv envoy_service_proc_v3.ExternalProcessor_ProcessServer) error {
	ctx := srv.Context()
	processor := s.factory.NewProcessor()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req, err := srv.Recv()
		if err != nil {
			if status.Code(err) == codes.Canceled || errors.Is(err, io.EOF) {
				return nil
			}
			s.log.Error().Err(err).Msg("failed to receive request")
			return status.Errorf(codes.Unknown, "cannot receive stream request: %v", err)
		}

		go func() {
			start := time.Now()
			resp := s.processOne(processor, req)
			s.log.Trace().
				Dur("duration", time.Since(start)).
				Interface("request", req).
				Interface("response", resp).
				Msg("request processed")
			if err := srv.Send(resp); err != nil {
				s.log.Error().Err(err).Msg("failed to send response")
			}
		}()
	}
}

func (s *Server) processOne(
	processor Processor,
	req *envoy_service_proc_v3.ProcessingRequest,
) *envoy_service_proc_v3.ProcessingResponse {
	s.log.Debug().
		Interface("request", req.Request).
		Type("request_type", req.Request).
		Msg("processing request")

	switch v := req.Request.(type) {
	case *envoy_service_proc_v3.ProcessingRequest_RequestHeaders:
		return s.handleRequestHeaders(processor, req, v.RequestHeaders)
	case *envoy_service_proc_v3.ProcessingRequest_ResponseHeaders:
		return s.handleResponseHeaders(processor, req, v.ResponseHeaders)
	case *envoy_service_proc_v3.ProcessingRequest_RequestBody:
		return s.handleRequestBody(processor, req, v.RequestBody)
	case *envoy_service_proc_v3.ProcessingRequest_ResponseBody:
		return s.handleResponseBody(processor, req, v.ResponseBody)
	case *envoy_service_proc_v3.ProcessingRequest_RequestTrailers:
		return s.handleRequestTrailers(processor, req, v.RequestTrailers)
	case *envoy_service_proc_v3.ProcessingRequest_ResponseTrailers:
		return s.handleResponseTrailers(processor, req, v.ResponseTrailers)
	default:
		s.log.Warn().
			Interface("request", req.Request).
			Type("request_type", v).
			Msg("unknown request type")
		return &envoy_service_proc_v3.ProcessingResponse{}
	}
}

func (s *Server) handleRequestHeaders(
	processor Processor,
	req *envoy_service_proc_v3.ProcessingRequest,
	h *envoy_service_proc_v3.HttpHeaders,
) *envoy_service_proc_v3.ProcessingResponse {
	ctx := &RequestContext{
		Attributes:  req.GetAttributes(),
		Headers:     parseHeaders(h),
		EndOfStream: h.GetEndOfStream(),
	}

	result := processor.ProcessRequestHeaders(ctx)
	return buildHeadersResponse(result, func(resp *envoy_service_proc_v3.HeadersResponse) *envoy_service_proc_v3.ProcessingResponse {
		return &envoy_service_proc_v3.ProcessingResponse{
			Response: &envoy_service_proc_v3.ProcessingResponse_RequestHeaders{
				RequestHeaders: resp,
			},
		}
	})
}

func (s *Server) handleResponseHeaders(
	processor Processor,
	req *envoy_service_proc_v3.ProcessingRequest,
	h *envoy_service_proc_v3.HttpHeaders,
) *envoy_service_proc_v3.ProcessingResponse {
	ctx := &RequestContext{
		Attributes:  req.GetAttributes(),
		Headers:     parseHeaders(h),
		EndOfStream: h.GetEndOfStream(),
	}

	result := processor.ProcessResponseHeaders(ctx)
	return buildHeadersResponse(result, func(resp *envoy_service_proc_v3.HeadersResponse) *envoy_service_proc_v3.ProcessingResponse {
		return &envoy_service_proc_v3.ProcessingResponse{
			Response: &envoy_service_proc_v3.ProcessingResponse_ResponseHeaders{
				ResponseHeaders: resp,
			},
		}
	})
}

func (s *Server) handleRequestBody(
	processor Processor,
	req *envoy_service_proc_v3.ProcessingRequest,
	b *envoy_service_proc_v3.HttpBody,
) *envoy_service_proc_v3.ProcessingResponse {
	ctx := &RequestContext{
		Attributes:  req.GetAttributes(),
		EndOfStream: b.GetEndOfStream(),
	}

	result := processor.ProcessRequestBody(ctx, b.GetBody(), b.GetEndOfStream())
	return buildBodyResponse(result, func(resp *envoy_service_proc_v3.BodyResponse) *envoy_service_proc_v3.ProcessingResponse {
		return &envoy_service_proc_v3.ProcessingResponse{
			Response: &envoy_service_proc_v3.ProcessingResponse_RequestBody{
				RequestBody: resp,
			},
		}
	})
}

func (s *Server) handleResponseBody(
	processor Processor,
	req *envoy_service_proc_v3.ProcessingRequest,
	b *envoy_service_proc_v3.HttpBody,
) *envoy_service_proc_v3.ProcessingResponse {
	ctx := &RequestContext{
		Attributes:  req.GetAttributes(),
		EndOfStream: b.GetEndOfStream(),
	}

	result := processor.ProcessResponseBody(ctx, b.GetBody(), b.GetEndOfStream())
	return buildBodyResponse(result, func(resp *envoy_service_proc_v3.BodyResponse) *envoy_service_proc_v3.ProcessingResponse {
		return &envoy_service_proc_v3.ProcessingResponse{
			Response: &envoy_service_proc_v3.ProcessingResponse_ResponseBody{
				ResponseBody: resp,
			},
		}
	})
}

func (s *Server) handleRequestTrailers(
	processor Processor,
	req *envoy_service_proc_v3.ProcessingRequest,
	_ *envoy_service_proc_v3.HttpTrailers,
) *envoy_service_proc_v3.ProcessingResponse {
	ctx := &RequestContext{
		Attributes: req.GetAttributes(),
	}

	result := processor.ProcessRequestTrailers(ctx)
	return buildTrailersResponse(result, func(resp *envoy_service_proc_v3.TrailersResponse) *envoy_service_proc_v3.ProcessingResponse {
		return &envoy_service_proc_v3.ProcessingResponse{
			Response: &envoy_service_proc_v3.ProcessingResponse_RequestTrailers{
				RequestTrailers: resp,
			},
		}
	})
}

func (s *Server) handleResponseTrailers(
	processor Processor,
	req *envoy_service_proc_v3.ProcessingRequest,
	_ *envoy_service_proc_v3.HttpTrailers,
) *envoy_service_proc_v3.ProcessingResponse {
	ctx := &RequestContext{
		Attributes: req.GetAttributes(),
	}

	result := processor.ProcessResponseTrailers(ctx)
	return buildTrailersResponse(result, func(resp *envoy_service_proc_v3.TrailersResponse) *envoy_service_proc_v3.ProcessingResponse {
		return &envoy_service_proc_v3.ProcessingResponse{
			Response: &envoy_service_proc_v3.ProcessingResponse_ResponseTrailers{
				ResponseTrailers: resp,
			},
		}
	})
}

// Helper functions for building responses.

func parseHeaders(h *envoy_service_proc_v3.HttpHeaders) http.Header {
	if h == nil {
		return make(http.Header)
	}
	headers := make(http.Header)
	for _, hdr := range h.GetHeaders().GetHeaders() {
		if raw := hdr.GetRawValue(); len(raw) > 0 {
			headers.Add(hdr.GetKey(), string(raw))
		} else {
			headers.Add(hdr.GetKey(), hdr.GetValue())
		}
	}
	return headers
}

func buildHeadersResponse(
	result *ProcessingResult,
	wrapper func(*envoy_service_proc_v3.HeadersResponse) *envoy_service_proc_v3.ProcessingResponse,
) *envoy_service_proc_v3.ProcessingResponse {
	if result.ImmediateResponse != nil {
		return &envoy_service_proc_v3.ProcessingResponse{
			Response: &envoy_service_proc_v3.ProcessingResponse_ImmediateResponse{
				ImmediateResponse: result.ImmediateResponse,
			},
		}
	}

	common := &envoy_service_proc_v3.CommonResponse{
		Status: result.Status,
	}
	if result.HeaderMutations != nil && len(result.HeaderMutations.SetHeaders) > 0 {
		common.HeaderMutation = &envoy_service_proc_v3.HeaderMutation{
			SetHeaders:    result.HeaderMutations.SetHeaders,
			RemoveHeaders: result.HeaderMutations.RemoveHeaders,
		}
	}
	return wrapper(&envoy_service_proc_v3.HeadersResponse{Response: common})
}

func buildBodyResponse(
	result *ProcessingResult,
	wrapper func(*envoy_service_proc_v3.BodyResponse) *envoy_service_proc_v3.ProcessingResponse,
) *envoy_service_proc_v3.ProcessingResponse {
	if result.ImmediateResponse != nil {
		return &envoy_service_proc_v3.ProcessingResponse{
			Response: &envoy_service_proc_v3.ProcessingResponse_ImmediateResponse{
				ImmediateResponse: result.ImmediateResponse,
			},
		}
	}

	return wrapper(&envoy_service_proc_v3.BodyResponse{
		Response: &envoy_service_proc_v3.CommonResponse{
			Status: result.Status,
		},
	})
}

func buildTrailersResponse(
	result *ProcessingResult,
	wrapper func(*envoy_service_proc_v3.TrailersResponse) *envoy_service_proc_v3.ProcessingResponse,
) *envoy_service_proc_v3.ProcessingResponse {
	if result.ImmediateResponse != nil {
		return &envoy_service_proc_v3.ProcessingResponse{
			Response: &envoy_service_proc_v3.ProcessingResponse_ImmediateResponse{
				ImmediateResponse: result.ImmediateResponse,
			},
		}
	}

	return wrapper(&envoy_service_proc_v3.TrailersResponse{})
}

// SetHeader creates a header value option that overwrites existing headers.
func SetHeader(key, value string) *envoy_api_v3_core.HeaderValueOption {
	return &envoy_api_v3_core.HeaderValueOption{
		Header: &envoy_api_v3_core.HeaderValue{
			Key:      strings.ToLower(key),
			Value:    value,
			RawValue: []byte(value),
		},
		AppendAction: envoy_api_v3_core.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
	}
}

func ParseIPFromAddress(addr string) (netip.Addr, error) {
	ip, errParse := netip.ParseAddr(strings.Trim(addr, "[]"))
	if errParse == nil {
		return ip, nil
	}
	ap, errParseAddrPort := netip.ParseAddrPort(addr)
	if errParseAddrPort == nil {
		return ap.Addr(), nil
	}
	return netip.Addr{}, oops.
		In("extproc").
		Code("PARSE_IP_FROM_ADDRESS_FAILED").
		With("addr", addr).
		Join(errParse, errParseAddrPort)
}
