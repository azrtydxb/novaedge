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
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// WebSocketProxy handles WebSocket connection proxying
type WebSocketProxy struct {
	logger   *zap.Logger
	upgrader websocket.Upgrader
}

// NewWebSocketProxy creates a new WebSocket proxy
func NewWebSocketProxy(logger *zap.Logger) *WebSocketProxy {
	return &WebSocketProxy{
		logger: logger,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			// Allow all origins for now - should be configurable in production
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
}

// IsWebSocketUpgrade checks if the request is a WebSocket upgrade request
func IsWebSocketUpgrade(r *http.Request) bool {
	return strings.ToLower(r.Header.Get("Upgrade")) == "websocket" &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// ProxyWebSocket handles proxying a WebSocket connection to a backend
func (p *WebSocketProxy) ProxyWebSocket(w http.ResponseWriter, r *http.Request, backendURL string) error {
	// Upgrade client connection
	clientConn, err := p.upgrader.Upgrade(w, r, nil)
	if err != nil {
		p.logger.Error("Failed to upgrade client connection",
			zap.Error(err),
		)
		return fmt.Errorf("failed to upgrade client connection: %w", err)
	}
	defer clientConn.Close()

	// Build backend WebSocket URL
	backendWSURL := buildBackendWebSocketURL(backendURL, r)

	// Connect to backend
	backendHeaders := http.Header{}
	copyWebSocketHeaders(r.Header, backendHeaders)

	backendConn, resp, err := websocket.DefaultDialer.Dial(backendWSURL, backendHeaders)
	if err != nil {
		p.logger.Error("Failed to connect to backend WebSocket",
			zap.String("backend", backendWSURL),
			zap.Error(err),
		)
		if resp != nil {
			p.logger.Error("Backend response",
				zap.Int("status", resp.StatusCode),
			)
		}
		// Send close frame to client
		_ = clientConn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "Backend connection failed"))
		return fmt.Errorf("failed to connect to backend: %w", err)
	}
	defer backendConn.Close()

	p.logger.Info("WebSocket connection established",
		zap.String("client", r.RemoteAddr),
		zap.String("backend", backendWSURL),
	)

	// Bidirectional proxy
	errChan := make(chan error, 2)

	// Client to backend
	go func() {
		errChan <- p.copyWebSocketMessages(clientConn, backendConn, "client->backend")
	}()

	// Backend to client
	go func() {
		errChan <- p.copyWebSocketMessages(backendConn, clientConn, "backend->client")
	}()

	// Wait for either direction to close or error
	err = <-errChan

	p.logger.Info("WebSocket connection closed",
		zap.String("client", r.RemoteAddr),
		zap.String("backend", backendWSURL),
		zap.Error(err),
	)

	return nil
}

// copyWebSocketMessages copies messages from src to dst
func (p *WebSocketProxy) copyWebSocketMessages(src, dst *websocket.Conn, direction string) error {
	for {
		messageType, message, err := src.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				p.logger.Debug("WebSocket closed normally",
					zap.String("direction", direction),
				)
				// Forward close message
				_ = dst.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return nil
			}
			if err == io.EOF {
				p.logger.Debug("WebSocket connection EOF",
					zap.String("direction", direction),
				)
				return nil
			}
			p.logger.Error("Error reading WebSocket message",
				zap.String("direction", direction),
				zap.Error(err),
			)
			return err
		}

		if err := dst.WriteMessage(messageType, message); err != nil {
			p.logger.Error("Error writing WebSocket message",
				zap.String("direction", direction),
				zap.Error(err),
			)
			return err
		}
	}
}

// buildBackendWebSocketURL constructs the backend WebSocket URL
func buildBackendWebSocketURL(backendURL string, r *http.Request) string {
	// Convert http:// to ws:// and https:// to wss://
	wsScheme := "ws"
	if strings.HasPrefix(backendURL, "https://") {
		wsScheme = "wss"
	}

	// Remove http:// or https:// prefix
	backendHost := strings.TrimPrefix(backendURL, "http://")
	backendHost = strings.TrimPrefix(backendHost, "https://")

	// Build full WebSocket URL with original path and query
	wsURL := fmt.Sprintf("%s://%s%s", wsScheme, backendHost, r.URL.RequestURI())

	return wsURL
}

// copyWebSocketHeaders copies relevant WebSocket headers from source to destination
func copyWebSocketHeaders(src, dst http.Header) {
	// Headers to copy for WebSocket connections
	headersToCopy := []string{
		"Sec-WebSocket-Protocol",
		"Sec-WebSocket-Extensions",
		"Sec-WebSocket-Key",
		"Sec-WebSocket-Version",
		"Origin",
		"User-Agent",
	}

	for _, header := range headersToCopy {
		if value := src.Get(header); value != "" {
			dst.Set(header, value)
		}
	}

	// Copy custom headers (X-* headers)
	for key := range src {
		if strings.HasPrefix(key, "X-") || strings.HasPrefix(key, "x-") {
			dst.Set(key, src.Get(key))
		}
	}
}
