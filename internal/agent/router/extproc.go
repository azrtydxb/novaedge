/*
Copyright 2024 NovaEdge Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package router

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ExtProc phase identifiers sent to the external processing service.
const (
	PhaseRequestHeaders  = "request_headers"
	PhaseRequestBody     = "request_body"
	PhaseResponseHeaders = "response_headers"
	PhaseResponseBody    = "response_body"
)

// Default configuration values for ExtProc.
const (
	DefaultExtProcTimeout = 200 * time.Millisecond
)

// ProcessingRequest is the message sent to the external processing gRPC service.
// It carries the request/response metadata for the current processing phase.
type ProcessingRequest struct {
	Headers map[string]string `json:"headers"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Body    []byte            `json:"body"`
	Phase   string            `json:"phase"`
}

// ProcessingResponse is the message returned by the external processing gRPC service.
// It contains header mutations and optional immediate-response directives.
type ProcessingResponse struct {
	HeadersToAdd          map[string]string `json:"headers_to_add"`
	HeadersToRemove       []string          `json:"headers_to_remove"`
	ImmediateResponseCode int32             `json:"immediate_response_code"`
	ImmediateResponseBody []byte            `json:"immediate_response_body"`
}

// ExtProcConfig holds configuration for the external processing middleware.
type ExtProcConfig struct {
	// Address is the gRPC server address (host:port) of the external processor.
	Address string

	// Timeout is the maximum duration to wait for the external processor.
	// Defaults to 200ms if zero.
	Timeout time.Duration

	// FailOpen controls behavior when the external service is unavailable.
	// When true (default), requests pass through unchanged on failure.
	// When false, a 500 error is returned.
	FailOpen bool

	// ProcessRequestHeaders enables sending request headers to the external service.
	ProcessRequestHeaders bool

	// ProcessRequestBody enables sending the request body to the external service.
	ProcessRequestBody bool

	// ProcessResponseHeaders enables sending response headers to the external service.
	ProcessResponseHeaders bool

	// ProcessResponseBody enables sending the response body to the external service.
	ProcessResponseBody bool
}

// DefaultExtProcConfig returns an ExtProcConfig with sensible defaults.
func DefaultExtProcConfig(address string) *ExtProcConfig {
	return &ExtProcConfig{
		Address:                address,
		Timeout:                DefaultExtProcTimeout,
		FailOpen:               true,
		ProcessRequestHeaders:  true,
		ProcessRequestBody:     false,
		ProcessResponseHeaders: true,
		ProcessResponseBody:    false,
	}
}

// extProcClient defines the interface for communicating with an external
// processing gRPC service. This abstraction enables testing with mock clients.
type extProcClient interface {
	ProcessRequest(ctx context.Context, req *ProcessingRequest) (*ProcessingResponse, error)
	Close() error
}

// grpcExtProcClient implements extProcClient using a real gRPC connection.
// The external processing service is defined as a simple unary RPC that
// accepts a JSON-encoded ProcessingRequest and returns a JSON-encoded
// ProcessingResponse. This avoids proto compilation dependencies while
// still leveraging gRPC transport, load balancing, and keepalive.
type grpcExtProcClient struct {
	conn *grpc.ClientConn
}

// ExternalProcessor_ServiceDesc is the gRPC service descriptor for the
// ExternalProcessor service, defined manually to avoid proto code generation.
var ExternalProcessor_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "novaedge.extproc.ExternalProcessor",
	Methods: []grpc.MethodDesc{
		{
			MethodName: "ProcessRequest",
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "extproc.proto",
}

// newGRPCExtProcClient creates a new gRPC client connecting to the given address.
func newGRPCExtProcClient(address string) (*grpcExtProcClient, error) {
	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	return &grpcExtProcClient{conn: conn}, nil
}

// ProcessRequest sends a processing request to the external gRPC service and
// returns the response. The request and response are JSON-encoded over the wire,
// matching the manual service descriptor.
func (c *grpcExtProcClient) ProcessRequest(ctx context.Context, req *ProcessingRequest) (*ProcessingResponse, error) {
	var resp ProcessingResponse
	fullMethod := "/novaedge.extproc.ExternalProcessor/ProcessRequest"
	err := c.conn.Invoke(ctx, fullMethod, req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// Close closes the underlying gRPC connection.
func (c *grpcExtProcClient) Close() error {
	return c.conn.Close()
}

// jsonCodec is a gRPC codec that uses JSON encoding. It is registered globally
// so that the ExtProc gRPC calls encode/decode using JSON rather than protobuf.
type jsonCodec struct{}

// Marshal encodes v as JSON bytes.
func (jsonCodec) Marshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// Unmarshal decodes JSON data into v.
func (jsonCodec) Unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// Name returns the codec name registered with gRPC.
func (jsonCodec) Name() string {
	return "json"
}

// ExtProcMiddleware is an HTTP middleware that sends request/response data
// to an external gRPC processing service for inspection and mutation.
type ExtProcMiddleware struct {
	config *ExtProcConfig
	client extProcClient
	logger *zap.Logger
	next   http.Handler
}

// NewExtProcMiddleware creates a new ExtProcMiddleware wrapping the given handler.
// It establishes a gRPC connection to the external processing service.
func NewExtProcMiddleware(cfg *ExtProcConfig, logger *zap.Logger, next http.Handler) (*ExtProcMiddleware, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultExtProcTimeout
	}

	client, err := newGRPCExtProcClient(cfg.Address)
	if err != nil {
		return nil, err
	}

	return &ExtProcMiddleware{
		config: cfg,
		client: client,
		logger: logger,
		next:   next,
	}, nil
}

// newExtProcMiddlewareWithClient creates an ExtProcMiddleware with an injected
// client, used for testing.
func newExtProcMiddlewareWithClient(cfg *ExtProcConfig, client extProcClient, logger *zap.Logger, next http.Handler) *ExtProcMiddleware {
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultExtProcTimeout
	}
	return &ExtProcMiddleware{
		config: cfg,
		client: client,
		logger: logger,
		next:   next,
	}
}

// ServeHTTP implements http.Handler. It intercepts the request, optionally
// sends request headers/body to the external processor, forwards to the next
// handler, then optionally processes response headers/body.
func (m *ExtProcMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Phase 1: Process request headers
	if m.config.ProcessRequestHeaders {
		if !m.processRequestPhase(w, r, PhaseRequestHeaders, nil) {
			return
		}
	}

	// Phase 2: Process request body
	if m.config.ProcessRequestBody && r.Body != nil {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			m.logger.Error("failed to read request body for extproc", zap.Error(err))
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		// Close the original body
		if closeErr := r.Body.Close(); closeErr != nil {
			m.logger.Debug("failed to close original request body", zap.Error(closeErr))
		}

		if !m.processRequestPhase(w, r, PhaseRequestBody, bodyBytes) {
			return
		}

		// Restore the body for downstream handlers
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	// Phase 3: Forward to next handler, capturing response if needed
	if m.config.ProcessResponseHeaders || m.config.ProcessResponseBody {
		ew := newExtProcResponseWriter(w)
		m.next.ServeHTTP(ew, r)
		m.processResponse(ew, r)
		return
	}

	// No response processing needed, forward directly
	m.next.ServeHTTP(w, r)
}

// processRequestPhase sends request data to the external processor for the
// given phase. It applies any returned header mutations to the request.
// Returns true if the request should continue, false if an immediate response
// was sent or an error occurred.
func (m *ExtProcMiddleware) processRequestPhase(w http.ResponseWriter, r *http.Request, phase string, body []byte) bool {
	headers := make(map[string]string, len(r.Header))
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	procReq := &ProcessingRequest{
		Headers: headers,
		Method:  r.Method,
		Path:    r.URL.Path,
		Body:    body,
		Phase:   phase,
	}

	ctx, cancel := context.WithTimeout(r.Context(), m.config.Timeout)
	defer cancel()

	resp, err := m.client.ProcessRequest(ctx, procReq)
	if err != nil {
		if m.config.FailOpen {
			m.logger.Warn("extproc service unavailable, failing open",
				zap.String("phase", phase),
				zap.Error(err))
			return true
		}
		m.logger.Error("extproc service unavailable, failing closed",
			zap.String("phase", phase),
			zap.Error(err))
		http.Error(w, "external processing service unavailable", http.StatusInternalServerError)
		return false
	}

	// Check for immediate response
	if resp.ImmediateResponseCode > 0 {
		w.WriteHeader(int(resp.ImmediateResponseCode))
		if len(resp.ImmediateResponseBody) > 0 {
			if _, writeErr := w.Write(resp.ImmediateResponseBody); writeErr != nil {
				m.logger.Error("failed to write immediate response body", zap.Error(writeErr))
			}
		}
		return false
	}

	// Apply header mutations to the request
	m.applyRequestHeaderMutations(r, resp)

	return true
}

// applyRequestHeaderMutations applies header add/remove mutations from the
// ProcessingResponse to the HTTP request.
func (m *ExtProcMiddleware) applyRequestHeaderMutations(r *http.Request, resp *ProcessingResponse) {
	for k, v := range resp.HeadersToAdd {
		r.Header.Set(k, v)
	}
	for _, k := range resp.HeadersToRemove {
		r.Header.Del(k)
	}
}

// processResponse handles response-phase external processing by sending
// captured response headers (and optionally body) to the external service
// and applying any mutations before flushing to the real client.
func (m *ExtProcMiddleware) processResponse(ew *extProcResponseWriter, r *http.Request) {
	// Process response headers
	if m.config.ProcessResponseHeaders {
		respHeaders := make(map[string]string, len(ew.headers))
		for k, v := range ew.headers {
			if len(v) > 0 {
				respHeaders[k] = v[0]
			}
		}

		procReq := &ProcessingRequest{
			Headers: respHeaders,
			Method:  r.Method,
			Path:    r.URL.Path,
			Phase:   PhaseResponseHeaders,
		}

		ctx, cancel := context.WithTimeout(r.Context(), m.config.Timeout)
		resp, err := m.client.ProcessRequest(ctx, procReq)
		cancel()

		if err != nil {
			if !m.config.FailOpen {
				m.logger.Error("extproc service unavailable during response, failing closed",
					zap.Error(err))
				// Cannot change status code if already written; just log
			} else {
				m.logger.Warn("extproc service unavailable during response, failing open",
					zap.Error(err))
			}
		} else {
			// Apply header mutations to captured response headers
			for k, v := range resp.HeadersToAdd {
				ew.headers.Set(k, v)
			}
			for _, k := range resp.HeadersToRemove {
				ew.headers.Del(k)
			}
		}
	}

	// Flush the captured response to the real writer
	ew.flush()
}

// Close releases resources held by the middleware, including the gRPC connection.
func (m *ExtProcMiddleware) Close() error {
	if m.client != nil {
		return m.client.Close()
	}
	return nil
}

// extProcResponseWriter captures the response status, headers, and body so
// that the ExtProc middleware can process and mutate them before sending to
// the actual client.
type extProcResponseWriter struct {
	underlying  http.ResponseWriter
	headers     http.Header
	statusCode  int
	body        bytes.Buffer
	wroteHeader bool
	mu          sync.Mutex
}

// newExtProcResponseWriter creates a response writer that captures the response.
func newExtProcResponseWriter(w http.ResponseWriter) *extProcResponseWriter {
	return &extProcResponseWriter{
		underlying: w,
		headers:    make(http.Header),
		statusCode: http.StatusOK,
	}
}

// Header returns the captured header map.
func (ew *extProcResponseWriter) Header() http.Header {
	return ew.headers
}

// WriteHeader captures the status code without writing to the underlying writer.
func (ew *extProcResponseWriter) WriteHeader(code int) {
	ew.mu.Lock()
	defer ew.mu.Unlock()
	if !ew.wroteHeader {
		ew.statusCode = code
		ew.wroteHeader = true
	}
}

// Write captures body bytes into the internal buffer.
func (ew *extProcResponseWriter) Write(b []byte) (int, error) {
	ew.mu.Lock()
	defer ew.mu.Unlock()
	if !ew.wroteHeader {
		ew.statusCode = http.StatusOK
		ew.wroteHeader = true
	}
	return ew.body.Write(b)
}

// Flush implements http.Flusher (no-op during capture, real flush at the end).
func (ew *extProcResponseWriter) Flush() {
	// No-op during capture phase; the real flush happens in flush().
}

// Hijack implements http.Hijacker for WebSocket upgrades.
func (ew *extProcResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := ew.underlying.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// flush writes the captured status, headers, and body to the underlying writer.
func (ew *extProcResponseWriter) flush() {
	ew.mu.Lock()
	defer ew.mu.Unlock()

	// Copy captured headers to the underlying writer
	dst := ew.underlying.Header()
	for k, vals := range ew.headers {
		for _, v := range vals {
			dst.Add(k, v)
		}
	}

	ew.underlying.WriteHeader(ew.statusCode)

	if ew.body.Len() > 0 {
		// Ignoring write error here since the client may have disconnected;
		// logging would need the logger which we don't carry on the writer.
		_, _ = ew.underlying.Write(ew.body.Bytes())
	}
}
