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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

// defaultMaxBodySize is the default maximum body size for transformation (1MB).
const defaultMaxBodySize int64 = 1 << 20

// BodyTransformConfig holds configuration for JSON body transformation.
type BodyTransformConfig struct {
	// Operations to apply (in order)
	Operations []TransformOperation
	// MaxBodySize is the maximum body size to buffer for transformation (default 1MB)
	MaxBodySize int64
}

// TransformOperation represents a single JSON transformation.
type TransformOperation struct {
	// Op is the operation type: "add", "remove", "replace", "move", "copy"
	Op string
	// Path is the JSON Pointer path (RFC 6901)
	Path string
	// Value is the value for add/replace operations (JSON-encoded)
	Value json.RawMessage
	// From is the source path for move/copy operations
	From string
}

// BodyTransformMiddleware transforms JSON request/response bodies.
type BodyTransformMiddleware struct {
	requestOps  *BodyTransformConfig
	responseOps *BodyTransformConfig
	logger      *zap.Logger
}

// NewBodyTransformMiddleware creates a new BodyTransformMiddleware with optional
// request and response transformation configurations.
func NewBodyTransformMiddleware(requestOps, responseOps *BodyTransformConfig, logger *zap.Logger) *BodyTransformMiddleware {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &BodyTransformMiddleware{
		requestOps:  requestOps,
		responseOps: responseOps,
		logger:      logger,
	}
}

// Wrap returns an http.Handler that applies JSON body transformations.
func (m *BodyTransformMiddleware) Wrap(next http.Handler) http.Handler {
	if m.requestOps == nil && m.responseOps == nil {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Transform request body if configured.
		if m.requestOps != nil && len(m.requestOps.Operations) > 0 && r.Body != nil {
			maxSize := m.requestOps.MaxBodySize
			if maxSize <= 0 {
				maxSize = defaultMaxBodySize
			}

			// Only transform JSON content.
			ct := r.Header.Get("Content-Type")
			if isJSONContentType(ct) {
				body, err := io.ReadAll(io.LimitReader(r.Body, maxSize+1))
				_ = r.Body.Close()
				if err != nil {
					m.logger.Debug("Failed to read request body for transformation", zap.Error(err))
					http.Error(w, "failed to read request body", http.StatusBadRequest)
					return
				}

				if int64(len(body)) > maxSize {
					m.logger.Debug("Request body exceeds max size for transformation",
						zap.Int64("maxSize", maxSize),
						zap.Int("bodySize", len(body)),
					)
					http.Error(w, "request body too large for transformation", http.StatusRequestEntityTooLarge)
					return
				}

				transformed, err := applyOperations(body, m.requestOps.Operations)
				if err != nil {
					m.logger.Debug("Failed to transform request body", zap.Error(err))
					// Pass through the original body on transformation error.
					r.Body = io.NopCloser(bytes.NewReader(body))
					r.ContentLength = int64(len(body))
				} else {
					r.Body = io.NopCloser(bytes.NewReader(transformed))
					r.ContentLength = int64(len(transformed))
					r.Header.Set("Content-Length", strconv.Itoa(len(transformed)))
				}
			}
		}

		// If response transformation is configured, use a buffered response writer.
		if m.responseOps != nil && len(m.responseOps.Operations) > 0 {
			brw := &bodyTransformResponseWriter{
				ResponseWriter: w,
				buf:            &bytes.Buffer{},
				maxSize:        m.responseOps.MaxBodySize,
				ops:            m.responseOps.Operations,
				logger:         m.logger,
			}
			if brw.maxSize <= 0 {
				brw.maxSize = defaultMaxBodySize
			}

			next.ServeHTTP(brw, r)
			brw.flush()
			return
		}

		next.ServeHTTP(w, r)
	})
}

// bodyTransformResponseWriter buffers the response body for transformation.
type bodyTransformResponseWriter struct {
	http.ResponseWriter
	buf        *bytes.Buffer
	maxSize    int64
	ops        []TransformOperation
	logger     *zap.Logger
	statusCode int
	headerSent bool
	isJSON     bool
	overflow   bool
}

func (rw *bodyTransformResponseWriter) WriteHeader(code int) {
	rw.statusCode = code
	ct := rw.Header().Get("Content-Type")
	rw.isJSON = isJSONContentType(ct)
	if !rw.isJSON {
		// Not JSON; write headers immediately and pass through.
		rw.headerSent = true
		rw.ResponseWriter.WriteHeader(code)
	}
}

func (rw *bodyTransformResponseWriter) Write(b []byte) (int, error) {
	if !rw.isJSON || rw.overflow {
		if !rw.headerSent {
			if rw.statusCode == 0 {
				rw.statusCode = http.StatusOK
			}
			rw.ResponseWriter.WriteHeader(rw.statusCode)
			rw.headerSent = true
		}
		return rw.ResponseWriter.Write(b)
	}

	// Buffer the response body for JSON transformation.
	if int64(rw.buf.Len()+len(b)) > rw.maxSize {
		// Exceeded max size; flush buffer as-is and switch to passthrough.
		rw.overflow = true
		rw.logger.Debug("Response body exceeds max size for transformation, passing through",
			zap.Int64("maxSize", rw.maxSize),
		)
		if !rw.headerSent {
			if rw.statusCode == 0 {
				rw.statusCode = http.StatusOK
			}
			rw.ResponseWriter.WriteHeader(rw.statusCode)
			rw.headerSent = true
		}
		// Write buffered content first.
		if rw.buf.Len() > 0 {
			_, _ = rw.ResponseWriter.Write(rw.buf.Bytes())
			rw.buf.Reset()
		}
		return rw.ResponseWriter.Write(b)
	}

	return rw.buf.Write(b)
}

func (rw *bodyTransformResponseWriter) flush() {
	if rw.overflow || !rw.isJSON || rw.headerSent {
		return
	}

	body := rw.buf.Bytes()
	transformed, err := applyOperations(body, rw.ops)
	if err != nil {
		rw.logger.Debug("Failed to transform response body", zap.Error(err))
		transformed = body
	}

	rw.Header().Set("Content-Length", strconv.Itoa(len(transformed)))
	if rw.statusCode == 0 {
		rw.statusCode = http.StatusOK
	}
	rw.ResponseWriter.WriteHeader(rw.statusCode)
	_, _ = rw.ResponseWriter.Write(transformed)
}

// applyOperations applies JSON Patch operations to a body.
func applyOperations(body []byte, ops []TransformOperation) ([]byte, error) {
	if len(body) == 0 {
		return body, nil
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("failed to parse JSON body: %w", err)
	}

	for _, op := range ops {
		var err error
		switch op.Op {
		case "add":
			err = applyAdd(data, op.Path, op.Value)
		case "remove":
			err = applyRemove(data, op.Path)
		case "replace":
			err = applyReplace(data, op.Path, op.Value)
		case "move":
			err = applyMove(data, op.From, op.Path)
		case "copy":
			err = applyCopy(data, op.From, op.Path)
		default:
			err = fmt.Errorf("unsupported operation: %s", op.Op)
		}
		if err != nil {
			return nil, fmt.Errorf("operation %s on path %q failed: %w", op.Op, op.Path, err)
		}
	}

	return json.Marshal(data)
}

// parsePointer parses a JSON Pointer (RFC 6901) into path segments.
// The leading "/" is required for non-empty pointers.
func parsePointer(pointer string) ([]string, error) {
	if pointer == "" {
		return nil, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("JSON Pointer must start with '/': %s", pointer)
	}
	parts := strings.Split(pointer[1:], "/")
	// Unescape ~1 -> / and ~0 -> ~
	for i, p := range parts {
		p = strings.ReplaceAll(p, "~1", "/")
		p = strings.ReplaceAll(p, "~0", "~")
		parts[i] = p
	}
	return parts, nil
}

// navigateToParent navigates the JSON object to the parent of the target path
// and returns the parent map and the final key.
func navigateToParent(data map[string]interface{}, pointer string) (map[string]interface{}, string, error) {
	parts, err := parsePointer(pointer)
	if err != nil {
		return nil, "", err
	}
	if len(parts) == 0 {
		return nil, "", fmt.Errorf("empty path not supported")
	}

	current := data
	for i := 0; i < len(parts)-1; i++ {
		next, ok := current[parts[i]]
		if !ok {
			return nil, "", fmt.Errorf("path segment %q not found", parts[i])
		}
		nextMap, ok := next.(map[string]interface{})
		if !ok {
			return nil, "", fmt.Errorf("path segment %q is not an object", parts[i])
		}
		current = nextMap
	}

	return current, parts[len(parts)-1], nil
}

// getValue retrieves the value at a JSON Pointer path.
func getValue(data map[string]interface{}, pointer string) (interface{}, error) {
	parent, key, err := navigateToParent(data, pointer)
	if err != nil {
		return nil, err
	}
	val, ok := parent[key]
	if !ok {
		return nil, fmt.Errorf("key %q not found", key)
	}
	return val, nil
}

// applyAdd adds a value at the given JSON Pointer path.
func applyAdd(data map[string]interface{}, path string, value json.RawMessage) error {
	parent, key, err := navigateToParent(data, path)
	if err != nil {
		return err
	}

	var val interface{}
	if err := json.Unmarshal(value, &val); err != nil {
		return fmt.Errorf("failed to unmarshal value: %w", err)
	}

	parent[key] = val
	return nil
}

// applyRemove removes the value at the given JSON Pointer path.
func applyRemove(data map[string]interface{}, path string) error {
	parent, key, err := navigateToParent(data, path)
	if err != nil {
		return err
	}

	if _, ok := parent[key]; !ok {
		return fmt.Errorf("key %q not found for removal", key)
	}

	delete(parent, key)
	return nil
}

// applyReplace replaces the value at the given JSON Pointer path.
func applyReplace(data map[string]interface{}, path string, value json.RawMessage) error {
	parent, key, err := navigateToParent(data, path)
	if err != nil {
		return err
	}

	if _, ok := parent[key]; !ok {
		return fmt.Errorf("key %q not found for replacement", key)
	}

	var val interface{}
	if err := json.Unmarshal(value, &val); err != nil {
		return fmt.Errorf("failed to unmarshal value: %w", err)
	}

	parent[key] = val
	return nil
}

// applyMove moves a value from one path to another.
func applyMove(data map[string]interface{}, from, path string) error {
	val, err := getValue(data, from)
	if err != nil {
		return fmt.Errorf("move source: %w", err)
	}

	if err := applyRemove(data, from); err != nil {
		return fmt.Errorf("move remove: %w", err)
	}

	valBytes, err := json.Marshal(val)
	if err != nil {
		return fmt.Errorf("move marshal: %w", err)
	}

	return applyAdd(data, path, valBytes)
}

// applyCopy copies a value from one path to another.
func applyCopy(data map[string]interface{}, from, path string) error {
	val, err := getValue(data, from)
	if err != nil {
		return fmt.Errorf("copy source: %w", err)
	}

	valBytes, err := json.Marshal(val)
	if err != nil {
		return fmt.Errorf("copy marshal: %w", err)
	}

	return applyAdd(data, path, valBytes)
}

// isJSONContentType checks if the content type indicates JSON.
func isJSONContentType(ct string) bool {
	ct = strings.TrimSpace(ct)
	if ct == "" {
		return false
	}
	ct = strings.ToLower(ct)
	return strings.HasPrefix(ct, "application/json") ||
		strings.HasSuffix(ct, "+json")
}
