package extproc

import (
	"net/http"
	"net/netip"

	envoy_api_v3_core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoy_service_proc_v3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"github.com/samber/oops"
	"google.golang.org/protobuf/types/known/structpb"
)

const envoyAttributesKey = "envoy.filters.http.ext_proc"

// RequestContext provides context for processing a single request phase.
type RequestContext struct {
	// Attributes from Envoy (e.g., source.address, request metadata).
	Attributes map[string]*structpb.Struct
	// Headers parsed into http.Header for convenience.
	Headers http.Header
	// EndOfStream indicates if this is the final message for this phase.
	EndOfStream bool
}

func (c *RequestContext) GetEnvoyAttributeValue(key string) (*structpb.Value, bool) {
	if attr, ok := c.Attributes[envoyAttributesKey]; ok {
		if field, ok := attr.Fields[key]; ok {
			return field, true
		}
	}
	return nil, false
}

func (c *RequestContext) GetEnvoyAttributeValueMap() map[string]any {
	if attr, ok := c.Attributes[envoyAttributesKey]; ok {
		out := make(map[string]any, len(attr.Fields))
		for key, value := range attr.Fields {
			out[key] = value.AsInterface()
		}
	}
	return nil
}

func (c *RequestContext) GetDownstreamRemoteIP() (netip.Addr, error) {
	if value, ok := c.GetEnvoyAttributeValue("source.address"); ok {
		ip, err := ParseIPFromAddress(value.GetStringValue())
		return oops.Wrap2(ip, err)
	}
	if c.Headers != nil {
		if v := c.Headers.Get(HeaderEnvoyExternalAddr); v != "" {
			ip, err := ParseIPFromAddress(v)
			return oops.Wrap2(ip, err)
		}
	}
	return netip.Addr{}, oops.
		With("attrs", c.Attributes).
		With("headers", c.Headers).
		New("downstream remote IP not found")
}

func (c *RequestContext) GetRequestID() string {
	if value, ok := c.GetEnvoyAttributeValue("request.id"); ok {
		return value.GetStringValue()
	}
	if c.Headers != nil {
		return c.Headers.Get("x-request-id")
	}
	return ""
}

// HeaderMutations represents header modifications to apply.
type HeaderMutations struct {
	SetHeaders    []*envoy_api_v3_core.HeaderValueOption
	RemoveHeaders []string
}

// ProcessingResult represents the outcome of processing a request phase.
type ProcessingResult struct {
	// Status determines whether to continue or respond immediately.
	Status envoy_service_proc_v3.CommonResponse_ResponseStatus
	// HeaderMutations contains header modifications to apply.
	HeaderMutations *HeaderMutations
	// ImmediateResponse, if non-nil, sends an immediate response to the client.
	ImmediateResponse *envoy_service_proc_v3.ImmediateResponse
}

// ContinueResult returns a ProcessingResult that continues processing.
func ContinueResult() *ProcessingResult {
	return &ProcessingResult{
		Status: envoy_service_proc_v3.CommonResponse_CONTINUE,
	}
}

// ContinueWithHeaders returns a ProcessingResult that continues with header mutations.
func ContinueWithHeaders(setHeaders []*envoy_api_v3_core.HeaderValueOption) *ProcessingResult {
	return &ProcessingResult{
		Status: envoy_service_proc_v3.CommonResponse_CONTINUE,
		HeaderMutations: &HeaderMutations{
			SetHeaders: setHeaders,
		},
	}
}

// Processor defines the interface for handling ext_proc requests.
// Each method handles a specific phase of the request/response lifecycle.
// Implementations can maintain state across phases within a single request.
type Processor interface {
	// ProcessRequestHeaders handles incoming request headers.
	// Called when Envoy receives headers from the downstream client.
	ProcessRequestHeaders(ctx *RequestContext) *ProcessingResult

	// ProcessRequestBody handles request body chunks.
	// May be called multiple times for chunked/streaming bodies.
	ProcessRequestBody(ctx *RequestContext, body []byte, endOfStream bool) *ProcessingResult

	// ProcessRequestTrailers handles request trailers.
	ProcessRequestTrailers(ctx *RequestContext) *ProcessingResult

	// ProcessResponseHeaders handles response headers from upstream.
	// Called when Envoy receives headers from the upstream service.
	ProcessResponseHeaders(ctx *RequestContext) *ProcessingResult

	// ProcessResponseBody handles response body chunks.
	// May be called multiple times for chunked/streaming bodies.
	ProcessResponseBody(ctx *RequestContext, body []byte, endOfStream bool) *ProcessingResult

	// ProcessResponseTrailers handles response trailers.
	ProcessResponseTrailers(ctx *RequestContext) *ProcessingResult
}

// ProcessorFactory creates new Processor instances for each incoming request stream.
// This allows processors to maintain per-request state.
type ProcessorFactory interface {
	// NewProcessor creates a new Processor for handling a single request lifecycle.
	NewProcessor() Processor
}

// BaseProcessor provides a default implementation that continues all phases.
// Embed this in custom processors to only override the phases you need.
type BaseProcessor struct{}

func (BaseProcessor) ProcessRequestHeaders(*RequestContext) *ProcessingResult {
	return ContinueResult()
}

func (BaseProcessor) ProcessRequestBody(*RequestContext, []byte, bool) *ProcessingResult {
	return ContinueResult()
}

func (BaseProcessor) ProcessRequestTrailers(*RequestContext) *ProcessingResult {
	return ContinueResult()
}

func (BaseProcessor) ProcessResponseHeaders(*RequestContext) *ProcessingResult {
	return ContinueResult()
}

func (BaseProcessor) ProcessResponseBody(*RequestContext, []byte, bool) *ProcessingResult {
	return ContinueResult()
}

func (BaseProcessor) ProcessResponseTrailers(*RequestContext) *ProcessingResult {
	return ContinueResult()
}

// Ensure BaseProcessor implements Processor.
var _ Processor = (*BaseProcessor)(nil)
