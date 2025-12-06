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

package standalone

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
version: "1.0"
global:
  logLevel: info
  metricsPort: 9090
  healthPort: 8080
listeners:
  - name: http
    port: 80
    protocol: HTTP
routes:
  - name: default
    match:
      path:
        type: PathPrefix
        value: /
    backends:
      - name: test-backend
backends:
  - name: test-backend
    endpoints:
      - address: localhost:8080
    lbPolicy: RoundRobin
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.Version != "1.0" {
		t.Errorf("Expected version 1.0, got %s", cfg.Version)
	}

	if len(cfg.Listeners) != 1 {
		t.Errorf("Expected 1 listener, got %d", len(cfg.Listeners))
	}

	if cfg.Listeners[0].Name != "http" {
		t.Errorf("Expected listener name 'http', got %s", cfg.Listeners[0].Name)
	}

	if cfg.Listeners[0].Port != 80 {
		t.Errorf("Expected listener port 80, got %d", cfg.Listeners[0].Port)
	}

	if len(cfg.Routes) != 1 {
		t.Errorf("Expected 1 route, got %d", len(cfg.Routes))
	}

	if len(cfg.Backends) != 1 {
		t.Errorf("Expected 1 backend, got %d", len(cfg.Backends))
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				Version: "1.0",
				Listeners: []ListenerConfig{
					{Name: "http", Port: 80, Protocol: "HTTP"},
				},
				Routes: []RouteConfig{
					{
						Name: "default",
						Backends: []RouteBackendRef{
							{Name: "backend1"},
						},
					},
				},
				Backends: []BackendConfig{
					{
						Name: "backend1",
						Endpoints: []EndpointConfig{
							{Address: "localhost:8080"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing version",
			config: Config{
				Listeners: []ListenerConfig{
					{Name: "http", Port: 80, Protocol: "HTTP"},
				},
			},
			wantErr: true,
		},
		{
			name: "missing listeners",
			config: Config{
				Version: "1.0",
			},
			wantErr: true,
		},
		{
			name: "invalid port",
			config: Config{
				Version: "1.0",
				Listeners: []ListenerConfig{
					{Name: "http", Port: 0, Protocol: "HTTP"},
				},
			},
			wantErr: true,
		},
		{
			name: "missing backend endpoints",
			config: Config{
				Version: "1.0",
				Listeners: []ListenerConfig{
					{Name: "http", Port: 80, Protocol: "HTTP"},
				},
				Backends: []BackendConfig{
					{Name: "backend1"},
				},
			},
			wantErr: true,
		},
		{
			name: "route references unknown backend",
			config: Config{
				Version: "1.0",
				Listeners: []ListenerConfig{
					{Name: "http", Port: 80, Protocol: "HTTP"},
				},
				Routes: []RouteConfig{
					{
						Name: "default",
						Backends: []RouteBackendRef{
							{Name: "nonexistent"},
						},
					},
				},
				Backends: []BackendConfig{
					{
						Name: "backend1",
						Endpoints: []EndpointConfig{
							{Address: "localhost:8080"},
						},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
