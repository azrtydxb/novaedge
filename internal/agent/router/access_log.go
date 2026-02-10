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
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// AccessLogFormat defines the format for access log entries
type AccessLogFormat string

const (
	// AccessLogFormatCLF is the Common Log Format
	AccessLogFormatCLF AccessLogFormat = "clf"
	// AccessLogFormatJSON is JSON format
	AccessLogFormatJSON AccessLogFormat = "json"
	// AccessLogFormatCustom is a custom template format
	AccessLogFormatCustom AccessLogFormat = "custom"
)

// AccessLogOutput defines where access logs are written
type AccessLogOutput string

const (
	// AccessLogOutputStdout writes to stdout
	AccessLogOutputStdout AccessLogOutput = "stdout"
	// AccessLogOutputFile writes to a file
	AccessLogOutputFile AccessLogOutput = "file"
	// AccessLogOutputBoth writes to both stdout and file
	AccessLogOutputBoth AccessLogOutput = "both"
)

// AccessLogEntry represents a single access log entry
type AccessLogEntry struct {
	ClientIP             string  `json:"client_ip"`
	Timestamp            string  `json:"timestamp"`
	Method               string  `json:"method"`
	URI                  string  `json:"uri"`
	Protocol             string  `json:"protocol"`
	StatusCode           int     `json:"status_code"`
	BodyBytesSent        int64   `json:"body_bytes_sent"`
	Duration             float64 `json:"duration_seconds"`
	UserAgent            string  `json:"user_agent"`
	Referer              string  `json:"referer"`
	RequestID            string  `json:"request_id"`
	UpstreamAddr         string  `json:"upstream_addr,omitempty"`
	UpstreamResponseTime float64 `json:"upstream_response_time,omitempty"`
	Host                 string  `json:"host"`
}

// AccessLogMiddleware logs HTTP request/response details
type AccessLogMiddleware struct {
	enabled        bool
	format         AccessLogFormat
	customTemplate *template.Template
	output         AccessLogOutput
	filterCodes    map[int]bool // status codes to log (empty = all)
	sampleRate     float64      // 0.0-1.0, 1.0 = log all
	logger         *zap.Logger
	writer         io.Writer
	fileWriter     *rotatingFileWriter
	mu             sync.RWMutex
	requestCounter atomic.Uint64
	bufPool        sync.Pool
}

// NewAccessLogMiddleware creates a new access log middleware from proto config
func NewAccessLogMiddleware(config *pb.AccessLogConfig, logger *zap.Logger) *AccessLogMiddleware {
	alm := &AccessLogMiddleware{
		filterCodes: make(map[int]bool),
		logger:      logger,
		bufPool: sync.Pool{
			New: func() interface{} {
				return new(bytes.Buffer)
			},
		},
	}

	if config == nil || !config.Enabled {
		alm.enabled = false
		return alm
	}

	alm.enabled = true

	// Set format
	switch strings.ToLower(config.Format) {
	case "json":
		alm.format = AccessLogFormatJSON
	case "custom":
		alm.format = AccessLogFormatCustom
		if config.Template != "" {
			tmpl, err := template.New("access-log").Parse(config.Template)
			if err != nil {
				logger.Error("Failed to parse access log template, falling back to CLF",
					zap.Error(err))
				alm.format = AccessLogFormatCLF
			} else {
				alm.customTemplate = tmpl
			}
		} else {
			// No template provided, fall back to CLF
			alm.format = AccessLogFormatCLF
		}
	default:
		alm.format = AccessLogFormatCLF
	}

	// Set output
	switch strings.ToLower(config.Output) {
	case "file":
		alm.output = AccessLogOutputFile
	case "both":
		alm.output = AccessLogOutputBoth
	default:
		alm.output = AccessLogOutputStdout
	}

	// Set up file writer if needed
	if alm.output == AccessLogOutputFile || alm.output == AccessLogOutputBoth {
		if config.FilePath != "" {
			maxSize := parseByteSize(config.MaxSize)
			if maxSize == 0 {
				maxSize = 100 * 1024 * 1024 // Default 100MB
			}
			maxBackups := int(config.MaxBackups)
			if maxBackups == 0 {
				maxBackups = 5
			}

			fw, err := newRotatingFileWriter(config.FilePath, maxSize, maxBackups)
			if err != nil {
				logger.Error("Failed to create access log file writer, falling back to stdout",
					zap.String("path", config.FilePath),
					zap.Error(err))
				alm.output = AccessLogOutputStdout
			} else {
				alm.fileWriter = fw
			}
		} else {
			alm.output = AccessLogOutputStdout
		}
	}

	// Set up combined writer
	switch alm.output {
	case AccessLogOutputFile:
		if alm.fileWriter != nil {
			alm.writer = alm.fileWriter
		} else {
			alm.writer = os.Stdout
		}
	case AccessLogOutputBoth:
		if alm.fileWriter != nil {
			alm.writer = io.MultiWriter(os.Stdout, alm.fileWriter)
		} else {
			alm.writer = os.Stdout
		}
	default:
		alm.writer = os.Stdout
	}

	// Build filter codes set
	for _, code := range config.FilterStatusCodes {
		alm.filterCodes[int(code)] = true
	}

	// Sample rate
	alm.sampleRate = config.SampleRate
	if alm.sampleRate <= 0 || alm.sampleRate > 1.0 {
		alm.sampleRate = 1.0 // Default: log everything
	}

	return alm
}

// IsEnabled returns whether access logging is enabled
func (alm *AccessLogMiddleware) IsEnabled() bool {
	return alm.enabled
}

// Close cleans up resources used by the middleware
func (alm *AccessLogMiddleware) Close() {
	if alm.fileWriter != nil {
		_ = alm.fileWriter.Close()
	}
}

// Wrap returns an http.Handler that logs access information
func (alm *AccessLogMiddleware) Wrap(next http.Handler) http.Handler {
	if !alm.enabled {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		// Check sampling
		if !alm.shouldSample() {
			next.ServeHTTP(w, r)
			return
		}

		// Wrap response writer to capture status code and bytes written
		alw := &accessLogResponseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(alw, r)

		// Check status code filter
		if !alm.shouldLog(alw.statusCode) {
			return
		}

		// Build log entry
		duration := time.Since(startTime)
		entry := AccessLogEntry{
			ClientIP:      extractClientIP(r),
			Timestamp:     startTime.UTC().Format(time.RFC3339Nano),
			Method:        r.Method,
			URI:           r.RequestURI,
			Protocol:      r.Proto,
			StatusCode:    alw.statusCode,
			BodyBytesSent: alw.bytesWritten,
			Duration:      duration.Seconds(),
			UserAgent:     r.UserAgent(),
			Referer:       r.Referer(),
			RequestID:     r.Header.Get("X-Request-ID"),
			Host:          r.Host,
		}

		// Format and write the log entry
		alm.writeEntry(entry)
	})
}

// shouldSample returns true if this request should be logged based on sample rate
func (alm *AccessLogMiddleware) shouldSample() bool {
	if alm.sampleRate >= 1.0 {
		return true
	}
	return rand.Float64() < alm.sampleRate
}

// shouldLog returns true if the given status code should be logged
func (alm *AccessLogMiddleware) shouldLog(statusCode int) bool {
	if len(alm.filterCodes) == 0 {
		return true // No filter, log all
	}
	return alm.filterCodes[statusCode]
}

// writeEntry formats and writes a log entry
func (alm *AccessLogMiddleware) writeEntry(entry AccessLogEntry) {
	poolVal := alm.bufPool.Get()
	buf, ok := poolVal.(*bytes.Buffer)
	if !ok {
		buf = new(bytes.Buffer)
	}
	buf.Reset()
	defer alm.bufPool.Put(buf)

	switch alm.format {
	case AccessLogFormatJSON:
		alm.writeJSON(buf, entry)
	case AccessLogFormatCustom:
		alm.writeCustom(buf, entry)
	default:
		alm.writeCLF(buf, entry)
	}

	buf.WriteByte('\n')

	alm.mu.Lock()
	_, err := alm.writer.Write(buf.Bytes())
	alm.mu.Unlock()

	if err != nil {
		alm.logger.Error("Failed to write access log entry", zap.Error(err))
	}
}

// writeCLF writes a Common Log Format entry
func (alm *AccessLogMiddleware) writeCLF(buf *bytes.Buffer, entry AccessLogEntry) {
	// CLF: client_ip - - [timestamp] "method uri protocol" status body_bytes_sent "referer" "user_agent" duration request_id
	ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
	if err != nil {
		ts = time.Now().UTC()
	}
	fmt.Fprintf(buf, `%s - - [%s] "%s %s %s" %d %d "%s" "%s" %.3f %s`,
		entry.ClientIP,
		ts.Format("02/Jan/2006:15:04:05 -0700"),
		entry.Method,
		entry.URI,
		entry.Protocol,
		entry.StatusCode,
		entry.BodyBytesSent,
		entry.Referer,
		entry.UserAgent,
		entry.Duration,
		entry.RequestID,
	)
}

// writeJSON writes a JSON formatted entry
func (alm *AccessLogMiddleware) writeJSON(buf *bytes.Buffer, entry AccessLogEntry) {
	encoder := json.NewEncoder(buf)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(entry) // nolint: we intentionally discard the error
	// Remove trailing newline added by Encode (we add our own)
	if buf.Len() > 0 && buf.Bytes()[buf.Len()-1] == '\n' {
		buf.Truncate(buf.Len() - 1)
	}
}

// writeCustom writes using a custom template
func (alm *AccessLogMiddleware) writeCustom(buf *bytes.Buffer, entry AccessLogEntry) {
	if alm.customTemplate == nil {
		alm.writeCLF(buf, entry)
		return
	}
	if err := alm.customTemplate.Execute(buf, entry); err != nil {
		// Fall back to CLF on template error
		buf.Reset()
		alm.writeCLF(buf, entry)
	}
}

// extractClientIP extracts the client IP from the request
func extractClientIP(r *http.Request) string {
	// Check X-Forwarded-For first
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Use the first IP in the chain
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}

	// Check X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// accessLogResponseWriter wraps http.ResponseWriter to capture status code and bytes
type accessLogResponseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
	wroteHeader  bool
}

func (w *accessLogResponseWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.statusCode = code
		w.wroteHeader = true
		w.ResponseWriter.WriteHeader(code)
	}
}

func (w *accessLogResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytesWritten += int64(n)
	return n, err
}

// Flush implements http.Flusher
func (w *accessLogResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap returns the underlying ResponseWriter
func (w *accessLogResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// rotatingFileWriter implements a simple log file writer with rotation
type rotatingFileWriter struct {
	path       string
	maxSize    int64
	maxBackups int
	mu         sync.Mutex
	file       *os.File
	size       int64
}

// newRotatingFileWriter creates a new rotating file writer
func newRotatingFileWriter(path string, maxSize int64, maxBackups int) (*rotatingFileWriter, error) {
	cleanPath := filepath.Clean(path)

	// Ensure parent directory exists
	dir := filepath.Dir(cleanPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	fw := &rotatingFileWriter{
		path:       cleanPath,
		maxSize:    maxSize,
		maxBackups: maxBackups,
	}

	if err := fw.openFile(); err != nil {
		return nil, err
	}

	return fw, nil
}

func (fw *rotatingFileWriter) openFile() error {
	f, err := os.OpenFile(fw.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("failed to open access log file: %w", err)
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to stat access log file: %w", err)
	}

	fw.file = f
	fw.size = info.Size()
	return nil
}

func (fw *rotatingFileWriter) Write(p []byte) (int, error) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	if fw.size+int64(len(p)) > fw.maxSize {
		if err := fw.rotate(); err != nil {
			return 0, err
		}
	}

	n, err := fw.file.Write(p)
	fw.size += int64(n)
	return n, err
}

func (fw *rotatingFileWriter) rotate() error {
	// Close current file
	if fw.file != nil {
		_ = fw.file.Close()
	}

	// Rotate existing backups
	for i := fw.maxBackups - 1; i >= 1; i-- {
		oldPath := fmt.Sprintf("%s.%d", fw.path, i)
		newPath := fmt.Sprintf("%s.%d", fw.path, i+1)
		_ = os.Rename(filepath.Clean(oldPath), filepath.Clean(newPath))
	}

	// Rename current to .1
	_ = os.Rename(fw.path, filepath.Clean(fmt.Sprintf("%s.1", fw.path)))

	// Remove oldest backup if beyond maxBackups
	oldest := filepath.Clean(fmt.Sprintf("%s.%d", fw.path, fw.maxBackups+1))
	_ = os.Remove(oldest)

	// Open new file
	return fw.openFile()
}

// Close closes the underlying file
func (fw *rotatingFileWriter) Close() error {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if fw.file != nil {
		return fw.file.Close()
	}
	return nil
}

// parseByteSize parses a size string like "100Mi" or "1Gi" into bytes
func parseByteSize(s string) int64 {
	if s == "" {
		return 0
	}

	s = strings.TrimSpace(s)

	multipliers := map[string]int64{
		"Ki": 1024,
		"Mi": 1024 * 1024,
		"Gi": 1024 * 1024 * 1024,
		"Ti": 1024 * 1024 * 1024 * 1024,
		"K":  1000,
		"M":  1000 * 1000,
		"G":  1000 * 1000 * 1000,
	}

	for suffix, multiplier := range multipliers {
		if strings.HasSuffix(s, suffix) {
			numStr := strings.TrimSuffix(s, suffix)
			num, err := strconv.ParseInt(numStr, 10, 64)
			if err != nil {
				return 0
			}
			return num * multiplier
		}
	}

	// Try parsing as plain bytes
	num, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return num
}
