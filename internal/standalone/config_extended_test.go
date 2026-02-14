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
	"testing"
	"time"
)

func TestRouteConfig_GetTimeout(t *testing.T) {
	tests := []struct {
		name     string
		timeout  string
		expected time.Duration
	}{
		{
			name:     "empty timeout returns default",
			timeout:  "",
			expected: 30 * time.Second,
		},
		{
			name:     "valid duration",
			timeout:  "60s",
			expected: 60 * time.Second,
		},
		{
			name:     "minute duration",
			timeout:  "5m",
			expected: 5 * time.Minute,
		},
		{
			name:     "millisecond duration",
			timeout:  "500ms",
			expected: 500 * time.Millisecond,
		},
		{
			name:     "invalid duration returns default",
			timeout:  "invalid",
			expected: 30 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &RouteConfig{Timeout: tt.timeout}
			result := r.GetTimeout()
			if result != tt.expected {
				t.Errorf("GetTimeout() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid minimal config",
			config: &Config{
				Version: "v1",
				Listeners: []ListenerConfig{
					{Name: "http", Port: 80, Protocol: "HTTP"},
				},
				Routes: []RouteConfig{
					{Name: "test", Backends: []RouteBackendRef{{Name: "backend1"}}},
				},
				Backends: []BackendConfig{
					{Name: "backend1", Endpoints: []EndpointConfig{{Address: "127.0.0.1:8080"}}},
				},
			},
			wantErr: false,
		},
		{
			name: "missing version",
			config: &Config{
				Version: "",
				Listeners: []ListenerConfig{
					{Name: "http", Port: 80, Protocol: "HTTP"},
				},
			},
			wantErr: true,
		},
		{
			name: "missing listeners",
			config: &Config{
				Version:   "v1",
				Listeners: []ListenerConfig{},
			},
			wantErr: true,
		},
		{
			name: "missing routes - empty slice still valid",
			config: &Config{
				Version: "v1",
				Listeners: []ListenerConfig{
					{Name: "http", Port: 80, Protocol: "HTTP"},
				},
				Routes: []RouteConfig{},
				Backends: []BackendConfig{
					{Name: "backend1", Endpoints: []EndpointConfig{{Address: "127.0.0.1:8080"}}},
				},
			},
			wantErr: false,
		},
		{
			name: "missing backends",
			config: &Config{
				Version: "v1",
				Listeners: []ListenerConfig{
					{Name: "http", Port: 80, Protocol: "HTTP"},
				},
				Routes: []RouteConfig{
					{Name: "test", Backends: []RouteBackendRef{{Name: "backend1"}}},
				},
				Backends: []BackendConfig{},
			},
			wantErr: true,
		},
		{
			name: "listener missing name",
			config: &Config{
				Version: "v1",
				Listeners: []ListenerConfig{
					{Port: 80, Protocol: "HTTP"},
				},
				Routes: []RouteConfig{
					{Name: "test", Backends: []RouteBackendRef{{Name: "backend1"}}},
				},
				Backends: []BackendConfig{
					{Name: "backend1", Endpoints: []EndpointConfig{{Address: "127.0.0.1:8080"}}},
				},
			},
			wantErr: true,
		},
		{
			name: "listener invalid port",
			config: &Config{
				Version: "v1",
				Listeners: []ListenerConfig{
					{Name: "http", Port: -1, Protocol: "HTTP"},
				},
				Routes: []RouteConfig{
					{Name: "test", Backends: []RouteBackendRef{{Name: "backend1"}}},
				},
				Backends: []BackendConfig{
					{Name: "backend1", Endpoints: []EndpointConfig{{Address: "127.0.0.1:8080"}}},
				},
			},
			wantErr: true,
		},
		{
			name: "backend missing name",
			config: &Config{
				Version: "v1",
				Listeners: []ListenerConfig{
					{Name: "http", Port: 80, Protocol: "HTTP"},
				},
				Routes: []RouteConfig{
					{Name: "test", Backends: []RouteBackendRef{{Name: "backend1"}}},
				},
				Backends: []BackendConfig{
					{Endpoints: []EndpointConfig{{Address: "127.0.0.1:8080"}}},
				},
			},
			wantErr: true,
		},
		{
			name: "backend missing endpoints",
			config: &Config{
				Version: "v1",
				Listeners: []ListenerConfig{
					{Name: "http", Port: 80, Protocol: "HTTP"},
				},
				Routes: []RouteConfig{
					{Name: "test", Backends: []RouteBackendRef{{Name: "backend1"}}},
				},
				Backends: []BackendConfig{
					{Name: "backend1"},
				},
			},
			wantErr: true,
		},
		{
			name: "route missing name",
			config: &Config{
				Version: "v1",
				Listeners: []ListenerConfig{
					{Name: "http", Port: 80, Protocol: "HTTP"},
				},
				Routes: []RouteConfig{
					{Backends: []RouteBackendRef{{Name: "backend1"}}},
				},
				Backends: []BackendConfig{
					{Name: "backend1", Endpoints: []EndpointConfig{{Address: "127.0.0.1:8080"}}},
				},
			},
			wantErr: true,
		},
		{
			name: "route missing backends",
			config: &Config{
				Version: "v1",
				Listeners: []ListenerConfig{
					{Name: "http", Port: 80, Protocol: "HTTP"},
				},
				Routes: []RouteConfig{
					{Name: "test"},
				},
				Backends: []BackendConfig{
					{Name: "backend1", Endpoints: []EndpointConfig{{Address: "127.0.0.1:8080"}}},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestStandaloneParseByteSize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int64
		wantErr  bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: 0,
			wantErr:  false,
		},
		{
			name:     "zero",
			input:    "0",
			expected: 0,
			wantErr:  false,
		},
		{
			name:     "plain number",
			input:    "1024",
			expected: 1024,
			wantErr:  false,
		},
		{
			name:     "kilobytes",
			input:    "10Ki",
			expected: 10 * 1024,
			wantErr:  false,
		},
		{
			name:     "megabytes",
			input:    "10Mi",
			expected: 10 * 1024 * 1024,
			wantErr:  false,
		},
		{
			name:     "gigabytes",
			input:    "1Gi",
			expected: 1024 * 1024 * 1024,
			wantErr:  false,
		},
		{
			name:    "k lowercase - not supported",
			input:   "10k",
			wantErr: true,
		},
		{
			name:    "m lowercase - not supported",
			input:   "10m",
			wantErr: true,
		},
		{
			name:    "invalid format",
			input:   "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := standaloneParseByteSize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("standaloneParseByteSize() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if result != tt.expected {
				t.Errorf("standaloneParseByteSize() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSafeInt32(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int32
	}{
		{name: "zero", input: 0, expected: 0},
		{name: "small value", input: 100, expected: 100},
		{name: "large value", input: 2147483647, expected: 2147483647},
		{name: "overflow clamped", input: 2147483648, expected: 2147483647},
		{name: "negative", input: -100, expected: -100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := safeInt32(tt.input)
			if result != tt.expected {
				t.Errorf("safeInt32() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSafeUint32(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected uint32
	}{
		{name: "zero", input: 0, expected: 0},
		{name: "small value", input: 100, expected: 100},
		{name: "large value", input: 4294967295, expected: 4294967295},
		{name: "negative clamped to zero", input: -100, expected: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := safeUint32(tt.input)
			if result != tt.expected {
				t.Errorf("safeUint32() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestMatchDomain(t *testing.T) {
	tests := []struct {
		pattern    string
		serverName string
		expected   bool
	}{
		{"example.com", "example.com", true},
		{"*.example.com", "api.example.com", true},
		{"*.example.com", "www.example.com", true},
		{"*.example.com", "example.com", false},
		{"*.example.com", "sub.api.example.com", false},
		{"example.com", "other.com", false},
		{"", "", true},
		{"test", "test", true},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := matchDomain(tt.pattern, tt.serverName)
			if result != tt.expected {
				t.Errorf("matchDomain(%q, %q) = %v, want %v", tt.pattern, tt.serverName, result, tt.expected)
			}
		})
	}
}
