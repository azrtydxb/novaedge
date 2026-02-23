//go:build !linux

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
	"fmt"
	"net"
)

// OriginalDst is not supported on non-Linux platforms.
func OriginalDst(_ net.Conn) (net.IP, int, error) {
	return nil, 0, fmt.Errorf("TPROXY SO_ORIGINAL_DST is only supported on Linux")
}

// CreateListener is not supported on non-Linux platforms.
func (tl *TransparentListener) CreateListener(_ context.Context) (net.Listener, error) {
	return nil, fmt.Errorf("transparent proxy listener is only supported on Linux")
}

// Start is not supported on non-Linux platforms.
func (tl *TransparentListener) Start(_ context.Context) error {
	return fmt.Errorf("transparent proxy listener is only supported on Linux")
}
