//go:build linux

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
	"fmt"
	"os"
	"strings"
)

const routeLocalnetPath = "/proc/sys/net/ipv4/conf/all/route_localnet"

// ensureRouteLocalnet verifies that net.ipv4.conf.all.route_localnet is enabled,
// which is required for DNAT to 127.0.0.1 on non-loopback interfaces.
//
// In Kubernetes, the sysctl-setup init container sets this before the agent starts.
// This function first checks the current value and returns early if already enabled.
// If not enabled, it attempts to write the value (works in standalone/privileged mode).
func ensureRouteLocalnet() error {
	val, err := os.ReadFile(routeLocalnetPath)
	if err != nil {
		return fmt.Errorf("failed to read route_localnet: %w", err)
	}
	if strings.TrimSpace(string(val)) == "1" {
		return nil
	}

	// Not enabled — attempt to set it (works in privileged containers or standalone mode).
	if err := os.WriteFile(routeLocalnetPath, []byte("1"), 0o600); err != nil {
		return fmt.Errorf("route_localnet is not enabled and cannot be set (read-only filesystem); "+
			"ensure the sysctl-setup init container is configured in the Helm chart: %w", err)
	}
	return nil
}
