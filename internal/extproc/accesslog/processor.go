package accesslog

import (
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mnixry/envoy-ext-procs/internal/extproc"
	"github.com/rs/zerolog"
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
	return &Processor{factory: f}
}

type requestInfo struct {
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

	mu       sync.Mutex
	logged   bool
	request  requestInfo
	response responseInfo
}

// ProcessRequestHeaders captures request metadata for logging.
func (p *Processor) ProcessRequestHeaders(ctx *extproc.RequestContext) *extproc.ProcessingResult {
	p.mu.Lock()
	defer p.mu.Unlock()

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

	p.request = requestInfo{
		RemoteIP: remoteIP,
		ClientIP: clientIP,
		Proto:    extproc.FirstNonEmpty(ctx.Headers.Get("x-forwarded-proto"), ctx.Headers.Get(":protocol")),
		Host:     extproc.FirstNonEmpty(ctx.Headers.Get("x-forwarded-host"), ctx.Headers.Get(":authority"), ctx.Headers.Get("host")),
		Method:   ctx.Headers.Get(":method"),
		URI:      extproc.FirstNonEmpty(ctx.Headers.Get("x-envoy-original-path"), ctx.Headers.Get(":path")),
		Headers:  p.headersToCaddyMap(ctx.Headers),
	}

	if cl := ctx.Headers.Get("content-length"); cl != "" {
		if n, err := strconv.ParseUint(cl, 10, 64); err == nil {
			p.request.Size = &n
		} else {
			p.factory.errLog.Warn().Err(err).Str("content-length", cl).Msg("failed to parse content length")
		}
	}

	return extproc.ContinueResult()
}

func (p *Processor) ProcessResponseHeaders(ctx *extproc.RequestContext) *extproc.ProcessingResult {
	p.mu.Lock()
	defer p.mu.Unlock()

	if statusStr := ctx.Headers.Get(":status"); statusStr != "" {
		if status, err := strconv.Atoi(statusStr); err == nil {
			p.response.Status = status
		}
	}

	if cl := ctx.Headers.Get("content-length"); cl != "" {
		if n, err := strconv.ParseUint(cl, 10, 64); err == nil {
			p.response.Size = &n
		} else {
			p.factory.errLog.Warn().Err(err).Str("content-length", cl).Msg("failed to parse content length")
		}
	}

	p.response.Headers = p.headersToCaddyMap(ctx.Headers)

	if ctx.EndOfStream {
		p.emitLog()
	}

	return extproc.ContinueResult()
}

func (p *Processor) ProcessResponseTrailers(ctx *extproc.RequestContext) *extproc.ProcessingResult {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.emitLog()
	return extproc.ContinueResult()
}

func (p *Processor) headersToCaddyMap(headers http.Header) map[string][]string {
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

func (p *Processor) emitLog() {
	if p.logged {
		return
	}
	p.logged = true

	level := zerolog.InfoLevel
	if p.response.Status >= 500 {
		level = zerolog.ErrorLevel
	}

	event := p.factory.accessLog.WithLevel(level)

	reqDict := zerolog.Dict().
		Str("remote_ip", p.request.RemoteIP).
		Str("client_ip", p.request.ClientIP).
		Str("proto", p.request.Proto).
		Str("method", p.request.Method).
		Str("host", p.request.Host).
		Str("uri", p.request.URI).
		Time("start_time", p.request.StartTime).
		Interface("size", p.request.Size).
		Interface("headers", p.request.Headers)

	event = event.Dict("request", reqDict)

	event = event.
		Dur("duration", time.Since(p.request.StartTime)).
		Interface("size", p.response.Size).
		Int("status", p.response.Status).
		Interface("resp_headers", p.response.Headers)

	event.Msg("request processed")
}

var _ extproc.ProcessorFactory = (*ProcessorFactory)(nil)

var _ extproc.Processor = (*Processor)(nil)
