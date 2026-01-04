// Package edgeone provides an ext_proc processor that validates requests
// originating from Tencent EdgeOne CDN and sets appropriate trust headers.
package edgeone

import (
	"fmt"
	"net/netip"

	envoy_api_v3_core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/mnixry/envoy-ext-procs/internal/extproc"
	"github.com/rs/zerolog"
)

const (
	HeaderTrusted          = "x-forwarded-from-edgeone"
	HeaderDownstreamRealIP = "eo-connecting-ip"
	HeaderXFF              = "x-forwarded-for"
	HeaderXRealIP          = "x-real-ip"
)

// TrustLevel indicates whether a request is from a trusted EdgeOne IP.
type TrustLevel string

const (
	TrustLevelNo      TrustLevel = "no"
	TrustLevelYes     TrustLevel = "yes"
	TrustLevelUnknown TrustLevel = "unknown"
)

// Validator checks if an IP address belongs to EdgeOne's network.
type Validator interface {
	IsEdgeOneIP(ip netip.Addr) (bool, error)
}

// ProcessorFactory creates EdgeOne processors.
type ProcessorFactory struct {
	validator Validator
	log       zerolog.Logger
}

// NewProcessorFactory creates a new EdgeOne ProcessorFactory.
func NewProcessorFactory(validator Validator, log zerolog.Logger) *ProcessorFactory {
	return &ProcessorFactory{
		validator: validator,
		log:       log.With().Str("processor", "edgeone").Logger(),
	}
}

// NewProcessor creates a new EdgeOne processor for a single request.
func (f *ProcessorFactory) NewProcessor() extproc.Processor {
	return &Processor{
		validator: f.validator,
		log:       f.log,
	}
}

// Processor handles EdgeOne IP validation for a single request.
type Processor struct {
	extproc.BaseProcessor
	validator Validator
	log       zerolog.Logger
}

// ProcessRequestHeaders validates the source IP and sets trust headers.
func (p *Processor) ProcessRequestHeaders(ctx *extproc.RequestContext) *extproc.ProcessingResult {
	remoteIP, err := extproc.GetDownstreamRemoteIP(ctx.Attributes, ctx.Headers)
	if err != nil {
		p.log.Warn().Err(err).Msg("failed to get downstream remote IP")
		return extproc.ContinueWithHeaders([]*envoy_api_v3_core.HeaderValueOption{
			extproc.SetHeader(HeaderTrusted, string(TrustLevelUnknown)),
		})
	}

	trustedVal := TrustLevelNo
	if isEdgeOne, err := p.validator.IsEdgeOneIP(remoteIP); err == nil && isEdgeOne {
		trustedVal = TrustLevelYes
	} else if err != nil {
		p.log.Error().
			Err(err).
			Str("remote_ip", remoteIP.String()).
			Msg("edgeone validation failed")
	}

	remoteIPStr := remoteIP.String()
	headers := []*envoy_api_v3_core.HeaderValueOption{
		extproc.SetHeader(HeaderTrusted, string(trustedVal)),
	}

	if trustedVal == TrustLevelNo {
		headers = append(headers,
			extproc.SetHeader(HeaderXFF, remoteIPStr),
			extproc.SetHeader(HeaderXRealIP, remoteIPStr),
		)
		return extproc.ContinueWithHeaders(headers)
	}

	// Trusted EdgeOne request - extract real client IP from EdgeOne header.
	if downstreamRaw := ctx.Headers.Get(HeaderDownstreamRealIP); downstreamRaw != "" {
		if downstreamIP, err := extproc.ParseIPFromAddress(downstreamRaw); err == nil {
			downstreamIPStr := downstreamIP.String()
			headers = append(headers,
				extproc.SetHeader(HeaderXFF, fmt.Sprintf("%s, %s", downstreamIPStr, remoteIPStr)),
				extproc.SetHeader(HeaderXRealIP, downstreamIPStr),
			)
			return extproc.ContinueWithHeaders(headers)
		} else {
			p.log.Warn().Err(err).Msg("failed to parse downstream IP")
		}
	}

	p.log.Warn().
		Str("header", HeaderDownstreamRealIP).
		Str("remote_ip", remoteIPStr).
		Msg("edgeone missing or invalid header")
	headers = append(headers,
		extproc.SetHeader(HeaderXFF, remoteIPStr),
		extproc.SetHeader(HeaderXRealIP, remoteIPStr),
	)
	return extproc.ContinueWithHeaders(headers)
}

// Ensure ProcessorFactory implements extproc.ProcessorFactory.
var _ extproc.ProcessorFactory = (*ProcessorFactory)(nil)

// Ensure Processor implements extproc.Processor.
var _ extproc.Processor = (*Processor)(nil)
