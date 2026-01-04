// Package accesslog provides an ext_proc processor that emits Caddy-style
// JSON access logs for each request/response cycle.
package accesslog

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mnixry/envoy-ext-procs/internal/extproc"
	"github.com/rs/zerolog"
)

// ProcessorFactory creates access log processors.
type ProcessorFactory struct {
	accessLog zerolog.Logger
	errLog    zerolog.Logger
	// includeRequestHeaders controls whether request headers are logged.
	includeRequestHeaders bool
	// includeResponseHeaders controls whether response headers are logged.
	includeResponseHeaders bool
	// excludeHeaders is a set of header names (lowercase) to exclude from logging.
	excludeHeaders map[string]struct{}
}

// Option configures a ProcessorFactory.
type Option func(*ProcessorFactory)

// WithRequestHeaders enables logging of request headers.
func WithRequestHeaders(include bool) Option {
	return func(f *ProcessorFactory) {
		f.includeRequestHeaders = include
	}
}

// WithResponseHeaders enables logging of response headers.
func WithResponseHeaders(include bool) Option {
	return func(f *ProcessorFactory) {
		f.includeResponseHeaders = include
	}
}

// WithExcludeHeaders sets headers to exclude from logging.
func WithExcludeHeaders(headers []string) Option {
	return func(f *ProcessorFactory) {
		f.excludeHeaders = make(map[string]struct{}, len(headers))
		for _, h := range headers {
			f.excludeHeaders[strings.ToLower(h)] = struct{}{}
		}
	}
}

// NewProcessorFactory creates a new access log ProcessorFactory.
func NewProcessorFactory(writer io.Writer, log zerolog.Logger, opts ...Option) *ProcessorFactory {
	f := &ProcessorFactory{
		// Create a dedicated logger for access logs with Caddy-style format.
		accessLog: zerolog.New(writer).With().
			Str("logger", "http.log.access").
			Logger(),
		errLog:                 log.With().Str("processor", "accesslog").Logger(),
		includeRequestHeaders:  true,
		includeResponseHeaders: true,
		excludeHeaders: map[string]struct{}{
			"authorization": {},
			"cookie":        {},
			"set-cookie":    {},
		},
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// NewProcessor creates a new access log processor for a single request.
func (f *ProcessorFactory) NewProcessor() extproc.Processor {
	return &Processor{
		factory:   f,
		startTime: time.Now(),
	}
}

// requestInfo holds request metadata for logging.
type requestInfo struct {
	remoteIP string
	proto    string
	method   string
	host     string
	uri      string
	headers  http.Header
}

// Processor handles access logging for a single request.
type Processor struct {
	extproc.BaseProcessor
	factory   *ProcessorFactory
	startTime time.Time

	mu       sync.Mutex
	logged   bool
	request  requestInfo
	status   int
	respHdrs http.Header
	size     int64
}

// ProcessRequestHeaders captures request metadata for logging.
func (p *Processor) ProcessRequestHeaders(ctx *extproc.RequestContext) *extproc.ProcessingResult {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.request = requestInfo{
		proto:  ctx.Headers.Get(":protocol"),
		method: ctx.Headers.Get(":method"),
		host:   ctx.Headers.Get(":authority"),
		uri:    ctx.Headers.Get(":path"),
	}

	// Extract remote IP from attributes or headers.
	if ip, err := extproc.GetDownstreamRemoteIP(ctx.Attributes, ctx.Headers); err == nil {
		p.request.remoteIP = ip.String()
	}

	// Default protocol if not set.
	if p.request.proto == "" {
		p.request.proto = "HTTP/1.1"
	}

	// Include request headers if enabled.
	if p.factory.includeRequestHeaders {
		p.request.headers = p.filterHeaders(ctx.Headers)
	}

	return extproc.ContinueResult()
}

// ProcessResponseHeaders captures response status and headers.
func (p *Processor) ProcessResponseHeaders(ctx *extproc.RequestContext) *extproc.ProcessingResult {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Extract status code.
	if statusStr := ctx.Headers.Get(":status"); statusStr != "" {
		if status, err := strconv.Atoi(statusStr); err == nil {
			p.status = status
		}
	}

	// Include response headers if enabled.
	if p.factory.includeResponseHeaders {
		p.respHdrs = p.filterHeaders(ctx.Headers)
	}

	// If end of stream, emit the log entry now.
	if ctx.EndOfStream {
		p.emitLog()
	}

	return extproc.ContinueResult()
}

// ProcessResponseBody tracks response size and emits log at end of stream.
func (p *Processor) ProcessResponseBody(ctx *extproc.RequestContext, body []byte, endOfStream bool) *extproc.ProcessingResult {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.size += int64(len(body))

	if endOfStream {
		p.emitLog()
	}

	return extproc.ContinueResult()
}

// ProcessResponseTrailers emits log entry if not already done.
func (p *Processor) ProcessResponseTrailers(ctx *extproc.RequestContext) *extproc.ProcessingResult {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.emitLog()
	return extproc.ContinueResult()
}

// filterHeaders returns a copy of headers with excluded headers removed.
func (p *Processor) filterHeaders(headers http.Header) http.Header {
	filtered := make(http.Header, len(headers))
	for key, values := range headers {
		// Skip pseudo-headers for the headers map (they're in dedicated fields).
		if strings.HasPrefix(key, ":") {
			continue
		}
		lowerKey := strings.ToLower(key)
		if _, excluded := p.factory.excludeHeaders[lowerKey]; excluded {
			continue
		}
		filtered[key] = values
	}
	return filtered
}

// emitLog writes the access log entry using zerolog. Must be called with p.mu held.
func (p *Processor) emitLog() {
	if p.logged {
		return
	}
	p.logged = true

	duration := time.Since(p.startTime)

	// Build the log event with Caddy-style structure.
	event := p.factory.accessLog.Info().
		Str("msg", "handled request").
		Int("status", p.status).
		Int64("size", p.size).
		Dur("duration", duration).
		Float64("duration_ms", float64(duration.Microseconds())/1000.0)

	// Add request object.
	event = event.Dict("request", zerolog.Dict().
		Str("remote_ip", p.request.remoteIP).
		Str("proto", p.request.proto).
		Str("method", p.request.method).
		Str("host", p.request.host).
		Str("uri", p.request.uri).
		Interface("headers", p.headersToMap(p.request.headers)),
	)

	// Add response headers if enabled.
	if p.factory.includeResponseHeaders && len(p.respHdrs) > 0 {
		event = event.Interface("resp_headers", p.headersToMap(p.respHdrs))
	}

	event.Send()
}

// headersToMap converts http.Header to a simple map for logging.
// Returns nil if headers is empty to omit the field.
func (p *Processor) headersToMap(headers http.Header) map[string][]string {
	if len(headers) == 0 {
		return nil
	}
	return headers
}

// Ensure ProcessorFactory implements extproc.ProcessorFactory.
var _ extproc.ProcessorFactory = (*ProcessorFactory)(nil)

// Ensure Processor implements extproc.Processor.
var _ extproc.Processor = (*Processor)(nil)
