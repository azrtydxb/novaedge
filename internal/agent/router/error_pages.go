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
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"text/template"
	"time"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// ErrorPageData contains template variables available in error page templates
type ErrorPageData struct {
	StatusCode int
	StatusText string
	RequestID  string
	Timestamp  string
}

// ErrorPageInterceptor intercepts error responses and serves custom error pages
type ErrorPageInterceptor struct {
	enabled     bool
	pages       map[int]*template.Template // status code -> compiled template
	defaultPage *template.Template         // fallback template
	logger      *zap.Logger
	mu          sync.RWMutex
}

// NewErrorPageInterceptor creates a new error page interceptor from proto config
func NewErrorPageInterceptor(config *pb.ErrorPageConfig, logger *zap.Logger) *ErrorPageInterceptor {
	epi := &ErrorPageInterceptor{
		pages:  make(map[int]*template.Template),
		logger: logger,
	}

	if config == nil || !config.Enabled {
		epi.enabled = false
		return epi
	}

	epi.enabled = true

	// Compile per-status-code templates
	for code, tmplStr := range config.Pages {
		tmpl, err := template.New(fmt.Sprintf("error-%d", code)).Parse(tmplStr)
		if err != nil {
			logger.Error("Failed to parse error page template",
				zap.Int32("status_code", code),
				zap.Error(err),
			)
			continue
		}
		epi.pages[int(code)] = tmpl
	}

	// Compile default template
	if config.DefaultPage != "" {
		tmpl, err := template.New("error-default").Parse(config.DefaultPage)
		if err != nil {
			logger.Error("Failed to parse default error page template", zap.Error(err))
			// Fall back to built-in default
			epi.defaultPage = defaultBuiltInTemplate()
		} else {
			epi.defaultPage = tmpl
		}
	} else {
		epi.defaultPage = defaultBuiltInTemplate()
	}

	return epi
}

// defaultBuiltInTemplate returns a simple, clean default error page template
func defaultBuiltInTemplate() *template.Template {
	const builtInHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.StatusCode}} {{.StatusText}}</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; display: flex; justify-content: center; align-items: center; min-height: 100vh; margin: 0; background: #f5f5f5; color: #333; }
    .container { text-align: center; padding: 2rem; }
    h1 { font-size: 4rem; margin: 0; color: #e74c3c; }
    p { font-size: 1.2rem; color: #666; }
    .meta { font-size: 0.85rem; color: #999; margin-top: 2rem; }
  </style>
</head>
<body>
  <div class="container">
    <h1>{{.StatusCode}}</h1>
    <p>{{.StatusText}}</p>
    <div class="meta">Request ID: {{.RequestID}} | {{.Timestamp}}</div>
  </div>
</body>
</html>`
	tmpl, _ := template.New("error-builtin").Parse(builtInHTML)
	return tmpl
}

// IsEnabled returns whether the error page interceptor is enabled
func (epi *ErrorPageInterceptor) IsEnabled() bool {
	return epi.enabled
}

// Wrap returns an http.Handler that intercepts error responses
func (epi *ErrorPageInterceptor) Wrap(next http.Handler) http.Handler {
	if !epi.enabled {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Create an intercepting response writer
		irw := &errorPageResponseWriter{
			ResponseWriter: w,
			interceptor:    epi,
			request:        r,
			buf:            &bytes.Buffer{},
		}

		next.ServeHTTP(irw, r)

		// If we intercepted, the response was already written by WriteHeader
		// If not intercepted, flush the buffered body
		if !irw.intercepted && irw.headerWritten {
			// Normal response - body was written directly
			return
		}
	})
}

// renderErrorPage renders a custom error page for the given status code
func (epi *ErrorPageInterceptor) renderErrorPage(w http.ResponseWriter, r *http.Request, statusCode int) bool {
	epi.mu.RLock()
	defer epi.mu.RUnlock()

	// Find template for this status code
	tmpl, ok := epi.pages[statusCode]
	if !ok {
		tmpl = epi.defaultPage
	}

	if tmpl == nil {
		return false
	}

	// Build template data
	data := ErrorPageData{
		StatusCode: statusCode,
		StatusText: http.StatusText(statusCode),
		RequestID:  r.Header.Get("X-Request-ID"),
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return false
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.WriteHeader(statusCode)
	_, _ = w.Write(buf.Bytes())
	return true
}

// errorPageResponseWriter intercepts WriteHeader calls to detect error status codes
type errorPageResponseWriter struct {
	http.ResponseWriter
	interceptor   *ErrorPageInterceptor
	request       *http.Request
	buf           *bytes.Buffer
	headerWritten bool
	intercepted   bool
	statusCode    int
}

func (w *errorPageResponseWriter) WriteHeader(code int) {
	if w.headerWritten {
		return
	}
	w.headerWritten = true
	w.statusCode = code

	// Only intercept 4xx and 5xx responses
	if code >= 400 {
		if w.interceptor.renderErrorPage(w.ResponseWriter, w.request, code) {
			w.intercepted = true
			return
		}
	}

	// Not intercepted - forward as-is
	w.ResponseWriter.WriteHeader(code)
}

func (w *errorPageResponseWriter) Write(b []byte) (int, error) {
	if !w.headerWritten {
		w.WriteHeader(http.StatusOK)
	}

	// If we intercepted the response with a custom error page, discard the original body
	if w.intercepted {
		return len(b), nil
	}

	return w.ResponseWriter.Write(b)
}

// Flush implements http.Flusher
func (w *errorPageResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap returns the underlying ResponseWriter
func (w *errorPageResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
