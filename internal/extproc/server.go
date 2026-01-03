package extproc

import (
	"context"
	"fmt"
	"io"
	"net/netip"
	"strings"

	envoy_api_v3_core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoy_service_proc_v3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"github.com/rs/zerolog"
	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	HeaderTrustedPrimary    = "x-forwarded-from-edgeone"
	HeaderTrustedSecondary  = "x-edgeone-trusted"
	HeaderDownstreamRealIP  = "eo-connecting-ip"
	HeaderXFF               = "x-forwarded-for"
	HeaderXRealIP           = "x-real-ip"
	HeaderEnvoyExternalAddr = "x-envoy-external-address"
)

type EdgeOneValidator interface {
	IsEdgeOneIP(ctx context.Context, ip netip.Addr) (bool, error)
}

type Server struct {
	envoy_service_proc_v3.UnimplementedExternalProcessorServer

	edgeone EdgeOneValidator
	log     zerolog.Logger
}

func New(edgeone EdgeOneValidator, log zerolog.Logger) *Server {
	return &Server{
		edgeone: edgeone,
		log:     log.With().Str("component", "extproc").Logger(),
	}
}

func (s *Server) Process(srv envoy_service_proc_v3.ExternalProcessor_ProcessServer) error {
	ctx := srv.Context()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req, err := srv.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Unknown, "cannot receive stream request: %v", err)
		}

		resp := s.processOne(ctx, req)
		if err := srv.Send(resp); err != nil {
			s.log.Error().Err(oops.Wrapf(err, "failed to send response")).Send()
		}
	}
}

func (s *Server) processOne(ctx context.Context, req *envoy_service_proc_v3.ProcessingRequest) *envoy_service_proc_v3.ProcessingResponse {
	switch v := req.Request.(type) {
	case *envoy_service_proc_v3.ProcessingRequest_RequestHeaders:
		return s.processRequestHeaders(ctx, req, v.RequestHeaders)
	case *envoy_service_proc_v3.ProcessingRequest_ResponseHeaders:
		return continueResponseHeaders()
	case *envoy_service_proc_v3.ProcessingRequest_RequestBody:
		return continueRequestBody()
	case *envoy_service_proc_v3.ProcessingRequest_ResponseBody:
		return continueResponseBody()
	case *envoy_service_proc_v3.ProcessingRequest_RequestTrailers:
		return continueRequestTrailers()
	case *envoy_service_proc_v3.ProcessingRequest_ResponseTrailers:
		return continueResponseTrailers()
	default:
		s.log.Warn().Str("type", fmt.Sprintf("%T", v)).Msg("unknown request type")
		return &envoy_service_proc_v3.ProcessingResponse{}
	}
}

func (s *Server) processRequestHeaders(
	ctx context.Context,
	req *envoy_service_proc_v3.ProcessingRequest,
	h *envoy_service_proc_v3.HttpHeaders,
) *envoy_service_proc_v3.ProcessingResponse {
	var headers []*envoy_api_v3_core.HeaderValue
	if h != nil {
		headers = h.GetHeaders().GetHeaders()
	}

	setHeaders := s.edgeOneHeaderMutations(ctx, req.GetAttributes(), headers)
	common := &envoy_service_proc_v3.CommonResponse{
		Status: envoy_service_proc_v3.CommonResponse_CONTINUE,
	}
	if len(setHeaders) > 0 {
		common.HeaderMutation = &envoy_service_proc_v3.HeaderMutation{
			SetHeaders: setHeaders,
		}
	}

	return &envoy_service_proc_v3.ProcessingResponse{
		Response: &envoy_service_proc_v3.ProcessingResponse_RequestHeaders{
			RequestHeaders: &envoy_service_proc_v3.HeadersResponse{Response: common},
		},
	}
}

func (s *Server) edgeOneHeaderMutations(
	ctx context.Context,
	attrs map[string]*structpb.Struct,
	headers []*envoy_api_v3_core.HeaderValue,
) []*envoy_api_v3_core.HeaderValueOption {
	remoteIP, ok := downstreamRemoteIP(attrs, headers)
	if !ok {
		s.log.Warn().
			Interface("attrs", attrs).
			Interface("headers", headers).
			Msg("downstream remote IP not found")
		return []*envoy_api_v3_core.HeaderValueOption{
			setHeaderOverwrite(HeaderTrustedPrimary, "no"),
			setHeaderOverwrite(HeaderTrustedSecondary, "no"),
		}
	}

	var trustedVal string
	if isEdgeOne, err := s.edgeone.IsEdgeOneIP(ctx, remoteIP); err != nil {
		s.log.Error().
			Err(oops.Wrapf(err, "edgeone validation failed")).
			Str("remote_ip", remoteIP.String()).
			Send()
		trustedVal = "no"
	} else if isEdgeOne {
		trustedVal = "yes"
	}

	remoteIPStr := remoteIP.String()
	out := []*envoy_api_v3_core.HeaderValueOption{
		setHeaderOverwrite(HeaderTrustedPrimary, trustedVal),
		setHeaderOverwrite(HeaderTrustedSecondary, trustedVal),
	}

	if trustedVal == "no" {
		out = append(out,
			setHeaderOverwrite(HeaderXFF, remoteIPStr),
			setHeaderOverwrite(HeaderXRealIP, remoteIPStr),
		)
		return out
	}

	if downstreamRaw, ok := getHeaderValue(headers, HeaderDownstreamRealIP); ok {
		if downstreamIP, ok := parseIPFromAddress(downstreamRaw); ok {
			downstreamIPStr := downstreamIP.String()
			out = append(out,
				setHeaderOverwrite(HeaderXFF, fmt.Sprintf("%s, %s", downstreamIPStr, remoteIPStr)),
				setHeaderOverwrite(HeaderXRealIP, downstreamIPStr),
			)
			return out
		}
	}

	s.log.Warn().
		Str("header", HeaderDownstreamRealIP).
		Str("remote_ip", remoteIPStr).
		Msg("edgeone missing or invalid header")
	out = append(out,
		setHeaderOverwrite(HeaderXFF, remoteIPStr),
		setHeaderOverwrite(HeaderXRealIP, remoteIPStr),
	)
	return out
}

func continueResponseHeaders() *envoy_service_proc_v3.ProcessingResponse {
	return &envoy_service_proc_v3.ProcessingResponse{
		Response: &envoy_service_proc_v3.ProcessingResponse_ResponseHeaders{
			ResponseHeaders: &envoy_service_proc_v3.HeadersResponse{
				Response: &envoy_service_proc_v3.CommonResponse{
					Status: envoy_service_proc_v3.CommonResponse_CONTINUE,
				},
			},
		},
	}
}

func continueRequestBody() *envoy_service_proc_v3.ProcessingResponse {
	return &envoy_service_proc_v3.ProcessingResponse{
		Response: &envoy_service_proc_v3.ProcessingResponse_RequestBody{
			RequestBody: &envoy_service_proc_v3.BodyResponse{
				Response: &envoy_service_proc_v3.CommonResponse{
					Status: envoy_service_proc_v3.CommonResponse_CONTINUE,
				},
			},
		},
	}
}

func continueResponseBody() *envoy_service_proc_v3.ProcessingResponse {
	return &envoy_service_proc_v3.ProcessingResponse{
		Response: &envoy_service_proc_v3.ProcessingResponse_ResponseBody{
			ResponseBody: &envoy_service_proc_v3.BodyResponse{
				Response: &envoy_service_proc_v3.CommonResponse{
					Status: envoy_service_proc_v3.CommonResponse_CONTINUE,
				},
			},
		},
	}
}

func continueRequestTrailers() *envoy_service_proc_v3.ProcessingResponse {
	return &envoy_service_proc_v3.ProcessingResponse{
		Response: &envoy_service_proc_v3.ProcessingResponse_RequestTrailers{
			RequestTrailers: &envoy_service_proc_v3.TrailersResponse{},
		},
	}
}

func continueResponseTrailers() *envoy_service_proc_v3.ProcessingResponse {
	return &envoy_service_proc_v3.ProcessingResponse{
		Response: &envoy_service_proc_v3.ProcessingResponse_ResponseTrailers{
			ResponseTrailers: &envoy_service_proc_v3.TrailersResponse{},
		},
	}
}

func setHeaderOverwrite(key, value string) *envoy_api_v3_core.HeaderValueOption {
	return &envoy_api_v3_core.HeaderValueOption{
		Header: &envoy_api_v3_core.HeaderValue{
			Key:      strings.ToLower(key),
			RawValue: []byte(value),
		},
		AppendAction: envoy_api_v3_core.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
	}
}

func getHeaderValue(headers []*envoy_api_v3_core.HeaderValue, key string) (string, bool) {
	want := strings.ToLower(key)
	for _, hdr := range headers {
		if strings.ToLower(hdr.GetKey()) != want {
			continue
		}
		if raw := hdr.GetRawValue(); len(raw) > 0 {
			return string(raw), true
		}
		return hdr.GetValue(), true
	}
	return "", false
}

func downstreamRemoteIP(attrs map[string]*structpb.Struct, headers []*envoy_api_v3_core.HeaderValue) (netip.Addr, bool) {
	// Try source.address from ext_proc attributes first.
	if v, ok := attrString(attrs, "source", "address"); ok {
		if ip, ok := parseIPFromAddress(v); ok {
			return ip, true
		}
	}
	// Fallback to x-envoy-external-address header.
	if v, ok := getHeaderValue(headers, HeaderEnvoyExternalAddr); ok {
		if ip, ok := parseIPFromAddress(v); ok {
			return ip, true
		}
	}
	return netip.Addr{}, false
}

func parseIPFromAddress(addr string) (netip.Addr, bool) {
	if ip, err := netip.ParseAddr(strings.Trim(addr, "[]")); err == nil {
		return ip, true
	} else if ap, err := netip.ParseAddrPort(addr); err == nil {
		return ap.Addr(), true
	} else {
		return netip.Addr{}, false
	}
}

func attrString(attrs map[string]*structpb.Struct, path ...string) (string, bool) {
	root := attrs[path[0]]
	if root == nil {
		return "", false
	}
	cur := root
	for _, key := range path[1 : len(path)-1] {
		cur = cur.Fields[key].GetStructValue()
		if cur == nil {
			return "", false
		}
	}
	if s := cur.Fields[path[len(path)-1]].GetStringValue(); s != "" {
		return s, true
	}
	return "", false
}
