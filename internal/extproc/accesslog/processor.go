package accesslog

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/mnixry/envoy-ext-procs/internal/extproc"
	"github.com/rs/zerolog"
	"github.com/samber/oops"
)

var sensitiveHeaders = []string{
	"cookie",
	"set-cookie",
	"authorization",
	"proxy-authorization",
}

type ProcessorFactory struct {
	accessLog      zerolog.Logger
	errLog         zerolog.Logger
	excludeHeaders []string
}

type Option func(*ProcessorFactory)

func WithExcludeHeaders(headers ...string) Option {
	return func(f *ProcessorFactory) {
		f.excludeHeaders = append(f.excludeHeaders, headers...)
	}
}

func NewProcessorFactory(writer io.Writer, log zerolog.Logger, opts ...Option) *ProcessorFactory {
	f := &ProcessorFactory{
		accessLog:      zerolog.New(writer),
		errLog:         log.With().Str("processor", "accesslog").Logger(),
		excludeHeaders: append([]string(nil), sensitiveHeaders...),
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// NewProcessor creates a new access log processor for a single request.
func (f *ProcessorFactory) NewProcessor() extproc.Processor {
	records, err := lru.New[string, *requestInfo](1000)
	if err != nil {
		f.errLog.Error().Err(err).Msg("failed to create records cache")
		return nil
	}
	return &Processor{
		factory: f,
		records: records,
	}
}

type requestInfo struct {
	ID        string              `json:"id"`
	RemoteIP  string              `json:"remote_ip"`
	ClientIP  string              `json:"client_ip"`
	Proto     string              `json:"proto"`
	Method    string              `json:"method"`
	Host      string              `json:"host"`
	URI       string              `json:"uri"`
	Headers   map[string][]string `json:"headers,omitempty"`
	StartTime time.Time           `json:"start_time"`
	Size      *uint64             `json:"size"`
}

type responseInfo struct {
	Headers map[string][]string `json:"headers,omitempty"`
	Size    *uint64             `json:"size"`
	Status  int                 `json:"status"`
}

type Processor struct {
	extproc.BaseProcessor
	factory *ProcessorFactory
	records *lru.Cache[string, *requestInfo]
}

// ProcessRequestHeaders captures request metadata for logging.
func (p *Processor) ProcessRequestHeaders(ctx *extproc.RequestContext) *extproc.ProcessingResult {
	requestID := ctx.GetRequestID()
	if requestID == "" {
		p.factory.errLog.Warn().Msg("request ID not found")
		return extproc.ContinueResult()
	}

	var remoteIP string
	if ip, err := ctx.GetDownstreamRemoteIP(); err == nil {
		remoteIP = ip.String()
	} else {
		p.factory.errLog.Warn().Err(err).Msg("failed to get downstream remote IP")
	}

	var clientIP string
	if xff := ctx.Headers.Get("x-forwarded-for"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		if ip, err := extproc.ParseIPFromAddress(first); err == nil {
			clientIP = ip.String()
		} else {
			p.factory.errLog.Warn().Err(err).Str("xff", xff).Msg("failed to parse client IP from X-Forwarded-For")
		}
	}

	info := &requestInfo{
		RemoteIP: remoteIP,
		ClientIP: clientIP,
		Proto:    extproc.FirstNonEmpty(ctx.Headers.Get("x-forwarded-proto"), ctx.Headers.Get(":protocol")),
		Host:     extproc.FirstNonEmpty(ctx.Headers.Get("x-forwarded-host"), ctx.Headers.Get(":authority"), ctx.Headers.Get("host")),
		Method:   ctx.Headers.Get(":method"),
		URI:      extproc.FirstNonEmpty(ctx.Headers.Get("x-envoy-original-path"), ctx.Headers.Get(":path")),
		Headers:  p.redactHeaders(ctx.Headers),
	}

	if cl := ctx.Headers.Get("content-length"); cl != "" {
		if n, err := strconv.ParseUint(cl, 10, 64); err == nil {
			info.Size = &n
		} else {
			p.factory.errLog.Warn().Err(err).Str("content-length", cl).Msg("failed to parse content length")
		}
	}

	p.records.Add(requestID, info)
	return extproc.ContinueResult()
}

func (p *Processor) ProcessResponseHeaders(ctx *extproc.RequestContext) *extproc.ProcessingResult {
	var request *requestInfo
	if id := ctx.GetRequestID(); id == "" {
		p.factory.errLog.Warn().Msg("request ID not found")
		return extproc.ContinueResult()
	} else if record, ok := p.records.Peek(id); !ok {
		p.factory.errLog.Warn().Str("id", id).Msg("request not found")
		return extproc.ContinueResult()
	} else {
		request = record
		p.records.Remove(id)
	}

	response := &responseInfo{
		Headers: p.redactHeaders(ctx.Headers),
	}

	if statusStr := ctx.Headers.Get(":status"); statusStr != "" {
		if status, err := strconv.Atoi(statusStr); err == nil {
			response.Status = status
		}
	}

	if cl := ctx.Headers.Get("content-length"); cl != "" {
		if n, err := strconv.ParseUint(cl, 10, 64); err == nil {
			response.Size = &n
		} else {
			p.factory.errLog.Warn().Err(err).Str("content-length", cl).Msg("failed to parse content length")
		}
	}

	if err := emitLog(p.factory.accessLog, request, response, ctx.GetEnvoyAttributeValueMap()); err != nil {
		p.factory.errLog.Error().Err(err).Msg("failed to emit access log")
	}
	return extproc.ContinueResult()
}

func (p *Processor) redactHeaders(headers http.Header) map[string][]string {
	out := make(map[string][]string, len(headers))
	for key, values := range headers {
		if !strings.HasPrefix(key, ":") {
			key = http.CanonicalHeaderKey(key)
		}
		if slices.ContainsFunc(p.factory.excludeHeaders, func(h string) bool {
			return strings.EqualFold(h, key)
		}) {
			out[key] = []string{"REDACTED"}
		} else {
			out[key] = values
		}
	}
	return out
}

func emitLog(log zerolog.Logger, request *requestInfo, response *responseInfo, attrs map[string]any) error {
	level := zerolog.InfoLevel
	if response.Status >= 500 {
		level = zerolog.ErrorLevel
	}
	event := log.WithLevel(level)

	if jsonReq, err := json.Marshal(request); err == nil {
		event = event.RawJSON("request", jsonReq)
	} else {
		return oops.With("request", request).Wrapf(err, "failed to marshal request")
	}

	if jsonAttr, err := json.Marshal(attrs); err == nil {
		event = event.RawJSON("attrs", jsonAttr)
	} else {
		return oops.With("attrs", attrs).Wrapf(err, "failed to marshal attributes")
	}

	event.
		Str("id", request.ID).
		Dur("duration", time.Since(request.StartTime)).
		Interface("size", response.Size).
		Int("status", response.Status).
		Interface("resp_headers", response.Headers).
		Msg("request processed")
	return nil
}

var _ extproc.ProcessorFactory = (*ProcessorFactory)(nil)

var _ extproc.Processor = (*Processor)(nil)
