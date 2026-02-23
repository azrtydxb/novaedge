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

package mesh

import (
	"context"
	"net"

	"go.uber.org/zap"
)

// ConnHandler processes an intercepted connection. It receives the original
// destination address and the accepted connection.
type ConnHandler func(ctx context.Context, conn net.Conn, origDst net.IP, origPort int)

// TransparentListener listens for TPROXY'd TCP connections on a specific port,
// extracts the original destination, and dispatches to a handler.
type TransparentListener struct {
	logger   *zap.Logger
	port     int32
	handler  ConnHandler
	listener net.Listener // optional pre-created listener (set before Start)
}

// NewTransparentListener creates a transparent listener on the given port.
func NewTransparentListener(logger *zap.Logger, port int32, handler ConnHandler) *TransparentListener {
	return &TransparentListener{
		logger:  logger.Named("transparent-listener"),
		port:    port,
		handler: handler,
	}
}

// SetListener sets a pre-created listener to use instead of creating one
// in Start(). This is used when the caller needs the listener's socket FD
// before accepting connections (e.g., for eBPF SOCKMAP registration).
func (tl *TransparentListener) SetListener(l net.Listener) {
	tl.listener = l
}
