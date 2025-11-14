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

package controller

import (
	"sync"

	"github.com/piwi3910/novaedge/internal/controller/snapshot"
)

var (
	configServer   *snapshot.Server
	configServerMu sync.RWMutex
)

// SetConfigServer sets the config server instance for reconcilers to use
func SetConfigServer(server *snapshot.Server) {
	configServerMu.Lock()
	defer configServerMu.Unlock()
	configServer = server
}

// GetConfigServer returns the config server instance
func GetConfigServer() *snapshot.Server {
	configServerMu.RLock()
	defer configServerMu.RUnlock()
	return configServer
}

// TriggerConfigUpdate triggers a config update for all nodes
func TriggerConfigUpdate() {
	server := GetConfigServer()
	if server != nil {
		server.TriggerUpdate("")
	}
}

// TriggerNodeConfigUpdate triggers a config update for a specific node
func TriggerNodeConfigUpdate(nodeName string) {
	server := GetConfigServer()
	if server != nil {
		server.TriggerUpdate(nodeName)
	}
}
