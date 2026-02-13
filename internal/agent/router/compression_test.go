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
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestCompressionMiddleware_GzipResponse(t *testing.T) {
	config := &pb.CompressionConfig{
		Enabled:    true,
		MinSize:    10, // Small threshold for testing
		Level:      6,
		Algorithms: []string{encodingGzip, "br"},
	}
	cm := NewCompressionMiddleware(config)

	body := strings.Repeat("Hello, this is a test body for compression. ", 50)
	handler := cm.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(body))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", encodingGzip)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	result := rec.Result()
	defer func() { _ = result.Body.Close() }()

	if result.Header.Get("Content-Encoding") != encodingGzip {
		t.Errorf("expected Content-Encoding: gzip, got %s", result.Header.Get("Content-Encoding"))
	}

	if result.Header.Get("Vary") != "Accept-Encoding" {
		t.Errorf("expected Vary: Accept-Encoding, got %s", result.Header.Get("Vary"))
	}

	// Decompress and verify
	reader, err := gzip.NewReader(result.Body)
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer func() { _ = reader.Close() }()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to decompress: %v", err)
	}

	if string(decompressed) != body {
		t.Errorf("decompressed body mismatch: got %d bytes, want %d bytes", len(decompressed), len(body))
	}
}

func TestCompressionMiddleware_BrotliResponse(t *testing.T) {
	config := &pb.CompressionConfig{
		Enabled:    true,
		MinSize:    10,
		Level:      4,
		Algorithms: []string{"br", encodingGzip},
	}
	cm := NewCompressionMiddleware(config)

	body := strings.Repeat("Brotli compression test body. ", 50)
	handler := cm.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(body))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "br, gzip")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	result := rec.Result()
	defer func() { _ = result.Body.Close() }()

	if result.Header.Get("Content-Encoding") != "br" {
		t.Errorf("expected Content-Encoding: br, got %s", result.Header.Get("Content-Encoding"))
	}

	// Decompress brotli
	reader := brotli.NewReader(result.Body)
	decompressed, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to decompress brotli: %v", err)
	}

	if string(decompressed) != body {
		t.Errorf("decompressed body mismatch: got %d bytes, want %d bytes", len(decompressed), len(body))
	}
}

func TestCompressionMiddleware_SkipAlreadyCompressed(t *testing.T) {
	config := &pb.CompressionConfig{
		Enabled:    true,
		MinSize:    10,
		Algorithms: []string{encodingGzip},
	}
	cm := NewCompressionMiddleware(config)

	body := strings.Repeat("already compressed content", 50)
	handler := cm.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Encoding", "br") // Already compressed
		_, _ = w.Write([]byte(body))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", encodingGzip)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	result := rec.Result()
	defer func() { _ = result.Body.Close() }()

	// Should not add a second Content-Encoding
	if result.Header.Get("Content-Encoding") != "br" {
		t.Errorf("expected Content-Encoding: br (unchanged), got %s", result.Header.Get("Content-Encoding"))
	}

	// Body should be unchanged (not double-compressed)
	respBody, _ := io.ReadAll(result.Body)
	if string(respBody) != body {
		t.Errorf("body should be unchanged when already compressed")
	}
}

func TestCompressionMiddleware_SkipExcludedContentType(t *testing.T) {
	config := &pb.CompressionConfig{
		Enabled:      true,
		MinSize:      10,
		Algorithms:   []string{encodingGzip},
		ExcludeTypes: []string{"image/*", "video/*"},
	}
	cm := NewCompressionMiddleware(config)

	body := strings.Repeat("image data simulated", 50)
	handler := cm.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte(body))
	}))

	req := httptest.NewRequest(http.MethodGet, "/image.png", nil)
	req.Header.Set("Accept-Encoding", encodingGzip)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	result := rec.Result()
	defer func() { _ = result.Body.Close() }()

	if result.Header.Get("Content-Encoding") == encodingGzip {
		t.Error("expected no compression for image/* content type")
	}

	respBody, _ := io.ReadAll(result.Body)
	if string(respBody) != body {
		t.Errorf("body should be unchanged for excluded content type")
	}
}

func TestCompressionMiddleware_MinimumSize(t *testing.T) {
	config := &pb.CompressionConfig{
		Enabled:    true,
		MinSize:    1024, // 1KB minimum
		Algorithms: []string{encodingGzip},
	}
	cm := NewCompressionMiddleware(config)

	// Body smaller than minimum size
	smallBody := "small body"
	handler := cm.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(smallBody))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", encodingGzip)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	result := rec.Result()
	defer func() { _ = result.Body.Close() }()

	// Should NOT be compressed (too small)
	if result.Header.Get("Content-Encoding") == encodingGzip {
		t.Error("expected no compression for body smaller than minSize")
	}

	respBody, _ := io.ReadAll(result.Body)
	if string(respBody) != smallBody {
		t.Errorf("body should be unchanged when below minSize")
	}
}

func TestCompressionMiddleware_AcceptEncodingNegotiation(t *testing.T) {
	tests := []struct {
		name           string
		acceptEncoding string
		algorithms     []string
		wantEncoding   string
	}{
		{
			name:           "client accepts gzip only",
			acceptEncoding: encodingGzip,
			algorithms:     []string{encodingGzip, "br"},
			wantEncoding:   encodingGzip,
		},
		{
			name:           "client accepts brotli only",
			acceptEncoding: "br",
			algorithms:     []string{encodingGzip, "br"},
			wantEncoding:   "br",
		},
		{
			name:           "client accepts both, server prefers br",
			acceptEncoding: "gzip, br",
			algorithms:     []string{"br", encodingGzip},
			wantEncoding:   "br",
		},
		{
			name:           "client accepts none we support",
			acceptEncoding: "deflate",
			algorithms:     []string{encodingGzip, "br"},
			wantEncoding:   "",
		},
		{
			name:           "no Accept-Encoding header",
			acceptEncoding: "",
			algorithms:     []string{encodingGzip},
			wantEncoding:   "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := &pb.CompressionConfig{
				Enabled:    true,
				MinSize:    10,
				Algorithms: tc.algorithms,
			}
			cm := NewCompressionMiddleware(config)

			body := strings.Repeat("test content for negotiation", 50)
			handler := cm.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write([]byte(body))
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.acceptEncoding != "" {
				req.Header.Set("Accept-Encoding", tc.acceptEncoding)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			result := rec.Result()
			defer func() { _ = result.Body.Close() }()

			got := result.Header.Get("Content-Encoding")
			if got != tc.wantEncoding {
				t.Errorf("Content-Encoding: got %q, want %q", got, tc.wantEncoding)
			}
		})
	}
}

func TestCompressionMiddleware_Disabled(t *testing.T) {
	// Nil config
	cm := NewCompressionMiddleware(nil)
	body := "test body"
	handler := cm.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", encodingGzip)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	result := rec.Result()
	defer func() { _ = result.Body.Close() }()

	if result.Header.Get("Content-Encoding") == encodingGzip {
		t.Error("expected no compression when config is nil")
	}

	// Disabled config
	config := &pb.CompressionConfig{Enabled: false}
	cm2 := NewCompressionMiddleware(config)
	handler2 := cm2.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))

	rec2 := httptest.NewRecorder()
	handler2.ServeHTTP(rec2, req)

	result2 := rec2.Result()
	defer func() { _ = result2.Body.Close() }()

	if result2.Header.Get("Content-Encoding") == encodingGzip {
		t.Error("expected no compression when disabled")
	}
}

func TestCompressResponseWriter_NoContent(t *testing.T) {
	config := &pb.CompressionConfig{
		Enabled:    true,
		MinSize:    10,
		Algorithms: []string{encodingGzip},
	}
	cm := NewCompressionMiddleware(config)

	handler := cm.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", encodingGzip)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	result := rec.Result()
	defer func() { _ = result.Body.Close() }()

	if result.StatusCode != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", result.StatusCode)
	}

	if result.Header.Get("Content-Encoding") == encodingGzip {
		t.Error("expected no compression for 204 No Content")
	}
}
