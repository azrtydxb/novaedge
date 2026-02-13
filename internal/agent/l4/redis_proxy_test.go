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

package l4

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

const (
	testRedisOK    = "+OK\r\n"
	testRedisPING  = "PING"
	testRedisSET   = "SET"
	testRedisAddr2 = "10.0.0.2"
	testRedisAddr3 = "10.0.0.3"
)

// parseBackendPort parses a port string and returns it as int32 with validation.
func parseBackendPort(t *testing.T, portStr string) int32 {
	t.Helper()
	port := 0
	_, _ = fmt.Sscanf(portStr, "%d", &port)
	if port < 0 || port > 65535 {
		t.Fatalf("Port %d out of valid range", port)
	}
	return int32(port) //nolint:gosec // port validated above
}

// --- RESP Protocol Parsing Tests ---

func TestRESPReader_SimpleString(t *testing.T) {
	input := testRedisOK
	reader := NewRESPReader(strings.NewReader(input))

	val, err := reader.ReadValue()
	if err != nil {
		t.Fatalf("Failed to read simple string: %v", err)
	}

	if val.Type != RESPTypeSimpleString {
		t.Errorf("Expected type SimpleString, got %c", byte(val.Type))
	}
	if val.Str != "OK" {
		t.Errorf("Expected 'OK', got %q", val.Str)
	}
	if !bytes.Equal(val.RawData, []byte(testRedisOK)) {
		t.Errorf("Raw data mismatch: %q", val.RawData)
	}
}

func TestRESPReader_Error(t *testing.T) {
	input := "-ERR unknown command\r\n"
	reader := NewRESPReader(strings.NewReader(input))

	val, err := reader.ReadValue()
	if err != nil {
		t.Fatalf("Failed to read error: %v", err)
	}

	if val.Type != RESPTypeError {
		t.Errorf("Expected type Error, got %c", byte(val.Type))
	}
	if val.Str != "ERR unknown command" {
		t.Errorf("Expected 'ERR unknown command', got %q", val.Str)
	}
}

func TestRESPReader_Integer(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int64
	}{
		{"positive", ":42\r\n", 42},
		{"zero", ":0\r\n", 0},
		{"negative", ":-1\r\n", -1},
		{"large", ":1000000\r\n", 1000000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reader := NewRESPReader(strings.NewReader(tc.input))
			val, err := reader.ReadValue()
			if err != nil {
				t.Fatalf("Failed to read integer: %v", err)
			}
			if val.Type != RESPTypeInteger {
				t.Errorf("Expected type Integer, got %c", byte(val.Type))
			}
			if val.Int != tc.expected {
				t.Errorf("Expected %d, got %d", tc.expected, val.Int)
			}
		})
	}
}

func TestRESPReader_BulkString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		isNil    bool
	}{
		{"simple", "$6\r\nfoobar\r\n", "foobar", false},
		{"empty", "$0\r\n\r\n", "", false},
		{"nil", "$-1\r\n", "", true},
		{"with spaces", "$11\r\nhello world\r\n", "hello world", false},
		{"binary safe", "$4\r\n\x00\x01\x02\x03\r\n", "\x00\x01\x02\x03", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reader := NewRESPReader(strings.NewReader(tc.input))
			val, err := reader.ReadValue()
			if err != nil {
				t.Fatalf("Failed to read bulk string: %v", err)
			}
			if tc.isNil {
				if !val.IsNil {
					t.Error("Expected nil bulk string")
				}
				return
			}
			if val.Type != RESPTypeBulkString {
				t.Errorf("Expected type BulkString, got %c", byte(val.Type))
			}
			if val.Str != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, val.Str)
			}
		})
	}
}

func TestRESPReader_Array(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		length   int
		isNil    bool
		elements []string
	}{
		{
			name:     "two bulk strings",
			input:    "*2\r\n$3\r\nfoo\r\n$3\r\nbar\r\n",
			length:   2,
			elements: []string{"foo", "bar"},
		},
		{
			name:   "empty array",
			input:  "*0\r\n",
			length: 0,
		},
		{
			name:  "nil array",
			input: "*-1\r\n",
			isNil: true,
		},
		{
			name:     "three element command",
			input:    "*3\r\n$3\r\nSET\r\n$3\r\nkey\r\n$5\r\nvalue\r\n",
			length:   3,
			elements: []string{"SET", "key", "value"},
		},
		{
			name:     "mixed types",
			input:    "*3\r\n:1\r\n:2\r\n:3\r\n",
			length:   3,
			elements: []string{"1", "2", "3"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reader := NewRESPReader(strings.NewReader(tc.input))
			val, err := reader.ReadValue()
			if err != nil {
				t.Fatalf("Failed to read array: %v", err)
			}
			if tc.isNil {
				if !val.IsNil {
					t.Error("Expected nil array")
				}
				return
			}
			if val.Type != RESPTypeArray {
				t.Errorf("Expected type Array, got %c", byte(val.Type))
			}
			if len(val.Array) != tc.length {
				t.Errorf("Expected array length %d, got %d", tc.length, len(val.Array))
			}
			for i, expected := range tc.elements {
				if i < len(val.Array) && val.Array[i].Str != expected {
					t.Errorf("Element %d: expected %q, got %q", i, expected, val.Array[i].Str)
				}
			}
		})
	}
}

func TestRESPReader_RawDataPreservation(t *testing.T) {
	// Verify that raw data is faithfully preserved for forwarding
	input := "*3\r\n$3\r\nSET\r\n$5\r\nmykey\r\n$7\r\nmyvalue\r\n"
	reader := NewRESPReader(strings.NewReader(input))

	val, err := reader.ReadValue()
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	if !bytes.Equal(val.RawData, []byte(input)) {
		t.Errorf("Raw data not preserved.\nExpected: %q\nGot:      %q", input, val.RawData)
	}
}

func TestRESPReader_ReadCommand_Inline(t *testing.T) {
	input := "PING\r\n"
	reader := NewRESPReader(strings.NewReader(input))

	parts, raw, err := reader.ReadCommand()
	if err != nil {
		t.Fatalf("Failed to read inline command: %v", err)
	}

	if len(parts) != 1 || parts[0] != testRedisPING {
		t.Errorf("Expected [PING], got %v", parts)
	}
	if len(raw) == 0 {
		t.Error("Expected raw data")
	}
}

func TestRESPReader_ReadCommand_InlineMultiArg(t *testing.T) {
	input := "SET mykey myvalue\r\n"
	reader := NewRESPReader(strings.NewReader(input))

	parts, _, err := reader.ReadCommand()
	if err != nil {
		t.Fatalf("Failed to read inline command: %v", err)
	}

	if len(parts) != 3 {
		t.Fatalf("Expected 3 parts, got %d: %v", len(parts), parts)
	}
	if parts[0] != testRedisSET || parts[1] != "mykey" || parts[2] != "myvalue" {
		t.Errorf("Expected [SET mykey myvalue], got %v", parts)
	}
}

func TestRESPReader_ReadCommand_RESP(t *testing.T) {
	input := "*2\r\n$4\r\nPING\r\n$5\r\nhello\r\n"
	reader := NewRESPReader(strings.NewReader(input))

	parts, raw, err := reader.ReadCommand()
	if err != nil {
		t.Fatalf("Failed to read RESP command: %v", err)
	}

	if len(parts) != 2 || parts[0] != testRedisPING || parts[1] != "hello" {
		t.Errorf("Expected [PING hello], got %v", parts)
	}
	if !bytes.Equal(raw, []byte(input)) {
		t.Errorf("Raw data mismatch")
	}
}

func TestRESPReader_ReadCommand_MultipleCommands(t *testing.T) {
	input := "*1\r\n$4\r\nPING\r\n*3\r\n$3\r\nSET\r\n$1\r\na\r\n$1\r\nb\r\n"
	reader := NewRESPReader(strings.NewReader(input))

	// Read first command
	parts1, _, err := reader.ReadCommand()
	if err != nil {
		t.Fatalf("Failed to read first command: %v", err)
	}
	if len(parts1) != 1 || parts1[0] != testRedisPING {
		t.Errorf("First command: expected [PING], got %v", parts1)
	}

	// Read second command
	parts2, _, err := reader.ReadCommand()
	if err != nil {
		t.Fatalf("Failed to read second command: %v", err)
	}
	if len(parts2) != 3 || parts2[0] != testRedisSET {
		t.Errorf("Second command: expected [SET a b], got %v", parts2)
	}
}

// --- RESP Writer Tests ---

func TestRESPWriter_SimpleString(t *testing.T) {
	var buf bytes.Buffer
	writer := NewRESPWriter(&buf)

	if err := writer.WriteSimpleString("OK"); err != nil {
		t.Fatalf("WriteSimpleString failed: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	if buf.String() != testRedisOK {
		t.Errorf("Expected '+OK\\r\\n', got %q", buf.String())
	}
}

func TestRESPWriter_Error(t *testing.T) {
	var buf bytes.Buffer
	writer := NewRESPWriter(&buf)

	if err := writer.WriteError("ERR test error"); err != nil {
		t.Fatalf("WriteError failed: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	if buf.String() != "-ERR test error\r\n" {
		t.Errorf("Expected '-ERR test error\\r\\n', got %q", buf.String())
	}
}

func TestRESPWriter_Integer(t *testing.T) {
	var buf bytes.Buffer
	writer := NewRESPWriter(&buf)

	if err := writer.WriteInteger(42); err != nil {
		t.Fatalf("WriteInteger failed: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	if buf.String() != ":42\r\n" {
		t.Errorf("Expected ':42\\r\\n', got %q", buf.String())
	}
}

func TestRESPWriter_BulkString(t *testing.T) {
	var buf bytes.Buffer
	writer := NewRESPWriter(&buf)

	if err := writer.WriteBulkString("foobar"); err != nil {
		t.Fatalf("WriteBulkString failed: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	expected := "$6\r\nfoobar\r\n"
	if buf.String() != expected {
		t.Errorf("Expected %q, got %q", expected, buf.String())
	}
}

func TestRESPWriter_NilBulkString(t *testing.T) {
	var buf bytes.Buffer
	writer := NewRESPWriter(&buf)

	if err := writer.WriteNilBulkString(); err != nil {
		t.Fatalf("WriteNilBulkString failed: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	if buf.String() != "$-1\r\n" {
		t.Errorf("Expected '$-1\\r\\n', got %q", buf.String())
	}
}

func TestRESPWriter_Array(t *testing.T) {
	var buf bytes.Buffer
	writer := NewRESPWriter(&buf)

	if err := writer.WriteArray(2); err != nil {
		t.Fatalf("WriteArray failed: %v", err)
	}
	if err := writer.WriteBulkString("foo"); err != nil {
		t.Fatalf("WriteBulkString failed: %v", err)
	}
	if err := writer.WriteBulkString("bar"); err != nil {
		t.Fatalf("WriteBulkString failed: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	expected := "*2\r\n$3\r\nfoo\r\n$3\r\nbar\r\n"
	if buf.String() != expected {
		t.Errorf("Expected %q, got %q", expected, buf.String())
	}
}

// --- EncodeCommand Tests ---

func TestEncodeCommand(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name:     "PING",
			args:     []string{"PING"},
			expected: "*1\r\n$4\r\nPING\r\n",
		},
		{
			name:     "SET key value",
			args:     []string{"SET", "mykey", "myvalue"},
			expected: "*3\r\n$3\r\nSET\r\n$5\r\nmykey\r\n$7\r\nmyvalue\r\n",
		},
		{
			name:     "GET key",
			args:     []string{"GET", "mykey"},
			expected: "*2\r\n$3\r\nGET\r\n$5\r\nmykey\r\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := EncodeCommand(tc.args...)
			if string(result) != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, string(result))
			}
		})
	}
}

func TestEncodeSimpleString(t *testing.T) {
	result := EncodeSimpleString("PONG")
	expected := "+PONG\r\n"
	if string(result) != expected {
		t.Errorf("Expected %q, got %q", expected, string(result))
	}
}

func TestEncodeError(t *testing.T) {
	result := EncodeError("ERR no backend")
	expected := "-ERR no backend\r\n"
	if string(result) != expected {
		t.Errorf("Expected %q, got %q", expected, string(result))
	}
}

// --- RESP Roundtrip Tests ---

func TestRESP_Roundtrip_WriteAndRead(t *testing.T) {
	var buf bytes.Buffer
	writer := NewRESPWriter(&buf)

	// Write a complete command
	if err := writer.WriteArray(3); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteBulkString("SET"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteBulkString("key"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteBulkString("value"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}

	// Read it back
	reader := NewRESPReader(&buf)
	val, err := reader.ReadValue()
	if err != nil {
		t.Fatalf("Failed to read back: %v", err)
	}

	if val.Type != RESPTypeArray || len(val.Array) != 3 {
		t.Fatalf("Expected 3-element array, got type=%c len=%d", byte(val.Type), len(val.Array))
	}
	if val.Array[0].Str != testRedisSET || val.Array[1].Str != "key" || val.Array[2].Str != "value" {
		t.Errorf("Unexpected values: %v", val.Array)
	}
}

// --- Redis Proxy Unit Tests ---

func TestRedisProxy_DefaultConfig(t *testing.T) {
	logger := zap.NewNop()

	proxy := NewRedisProxy(RedisProxyConfig{
		ListenerName: "test-defaults",
	}, logger)

	if proxy.config.ConnectTimeout != DefaultRedisConnectTimeout {
		t.Errorf("Expected default connect timeout %v, got %v",
			DefaultRedisConnectTimeout, proxy.config.ConnectTimeout)
	}
	if proxy.config.IdleTimeout != DefaultRedisIdleTimeout {
		t.Errorf("Expected default idle timeout %v, got %v",
			DefaultRedisIdleTimeout, proxy.config.IdleTimeout)
	}
	if proxy.config.PoolSize != DefaultRedisPoolSize {
		t.Errorf("Expected default pool size %d, got %d",
			DefaultRedisPoolSize, proxy.config.PoolSize)
	}
	if proxy.config.ReadTimeout != DefaultRedisReadTimeout {
		t.Errorf("Expected default read timeout %v, got %v",
			DefaultRedisReadTimeout, proxy.config.ReadTimeout)
	}
	if proxy.config.DrainTimeout != DefaultRedisDrainTimeout {
		t.Errorf("Expected default drain timeout %v, got %v",
			DefaultRedisDrainTimeout, proxy.config.DrainTimeout)
	}
}

func TestRedisProxy_PickBackend_RoundRobin(t *testing.T) {
	logger := zaptest.NewLogger(t)

	backends := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 6379, Ready: true},
		{Address: testRedisAddr2, Port: 6379, Ready: true},
		{Address: testRedisAddr3, Port: 6379, Ready: true},
	}

	proxy := NewRedisProxy(RedisProxyConfig{
		ListenerName: "test-rr",
		Backends:     backends,
		BackendName:  "redis-cluster",
	}, logger)

	// Pick backends and verify round-robin
	seen := make(map[string]int)
	for i := 0; i < 9; i++ {
		ep := proxy.pickBackend()
		if ep == nil {
			t.Fatal("pickBackend returned nil")
		}
		seen[ep.Address]++
	}

	// Each backend should be picked 3 times
	for _, backend := range backends {
		if seen[backend.Address] != 3 {
			t.Errorf("Expected backend %s to be picked 3 times, got %d",
				backend.Address, seen[backend.Address])
		}
	}
}

func TestRedisProxy_PickBackend_NoReady(t *testing.T) {
	logger := zaptest.NewLogger(t)

	backends := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 6379, Ready: false},
		{Address: testRedisAddr2, Port: 6379, Ready: false},
	}

	proxy := NewRedisProxy(RedisProxyConfig{
		ListenerName: "test-no-ready",
		Backends:     backends,
		BackendName:  "redis-cluster",
	}, logger)

	ep := proxy.pickBackend()
	if ep != nil {
		t.Errorf("Expected nil for no ready backends, got %v", ep)
	}
}

func TestRedisProxy_UpdateBackends(t *testing.T) {
	logger := zaptest.NewLogger(t)

	initialBackends := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 6379, Ready: true},
	}

	proxy := NewRedisProxy(RedisProxyConfig{
		ListenerName: "test-update",
		Backends:     initialBackends,
		BackendName:  "redis-cluster",
	}, logger)

	ep := proxy.pickBackend()
	if ep == nil || ep.Address != "10.0.0.1" {
		t.Fatal("Expected initial backend")
	}

	// Update backends
	newBackends := []*pb.Endpoint{
		{Address: testRedisAddr2, Port: 6379, Ready: true},
		{Address: testRedisAddr3, Port: 6379, Ready: true},
	}
	proxy.UpdateBackends(newBackends)

	// Verify new backends are used
	ep = proxy.pickBackend()
	if ep == nil {
		t.Fatal("Expected backend after update")
	}
	if ep.Address != testRedisAddr2 && ep.Address != testRedisAddr3 {
		t.Errorf("Expected updated backend, got %s", ep.Address)
	}

	// Verify pools were created for new backends
	if _, exists := proxy.pools[testRedisAddr2+":6379"]; !exists {
		t.Error("Expected pool for 10.0.0.2:6379")
	}
	if _, exists := proxy.pools[testRedisAddr3+":6379"]; !exists {
		t.Error("Expected pool for 10.0.0.3:6379")
	}
}

func TestRedisProxy_Drain(t *testing.T) {
	logger := zaptest.NewLogger(t)

	proxy := NewRedisProxy(RedisProxyConfig{
		ListenerName: "test-drain",
		Backends:     []*pb.Endpoint{{Address: "10.0.0.1", Port: 6379, Ready: true}},
		BackendName:  "redis-cluster",
	}, logger)

	if proxy.IsDraining() {
		t.Error("Proxy should not be draining initially")
	}

	proxy.Drain(100 * time.Millisecond)

	if !proxy.IsDraining() {
		t.Error("Proxy should be draining after Drain()")
	}
}

// --- Integration-style Tests with Mock Redis Backend ---

// mockRedisBackend simulates a Redis server for testing
func mockRedisBackend(t *testing.T) (net.Listener, string) {
	t.Helper()
	lc := net.ListenConfig{}
	listener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start mock Redis backend: %v", err)
	}

	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go handleMockRedisConn(conn)
		}
	}()

	addr := listener.Addr().String()
	return listener, addr
}

// handleMockRedisConn handles a single connection to the mock Redis backend
func handleMockRedisConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	reader := NewRESPReader(conn)
	for {
		parts, _, err := reader.ReadCommand()
		if err != nil {
			return
		}

		if len(parts) == 0 {
			continue
		}

		cmd := strings.ToUpper(parts[0])
		switch cmd {
		case "PING":
			if len(parts) > 1 {
				// PING with message returns the message as bulk string
				_, _ = fmt.Fprintf(conn, "$%d\r\n%s\r\n", len(parts[1]), parts[1])
			} else {
				_, _ = conn.Write(EncodeSimpleString("PONG"))
			}
		case "SET":
			_, _ = conn.Write(EncodeSimpleString("OK"))
		case "GET":
			if len(parts) > 1 && parts[1] == "existing" {
				_, _ = conn.Write([]byte("$5\r\nvalue\r\n"))
			} else {
				_, _ = conn.Write([]byte("$-1\r\n"))
			}
		case "QUIT":
			_, _ = conn.Write(EncodeSimpleString("OK"))
			return
		default:
			_, _ = conn.Write(EncodeError(fmt.Sprintf("ERR unknown command '%s'", cmd)))
		}
	}
}

func TestRedisProxy_HandleConnection_PingPong(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Start mock Redis backend
	backendListener, backendAddr := mockRedisBackend(t)
	defer func() { _ = backendListener.Close() }()

	host, portStr, _ := net.SplitHostPort(backendAddr)
	port := parseBackendPort(t, portStr)

	backends := []*pb.Endpoint{
		{Address: host, Port: port, Ready: true},
	}

	proxy := NewRedisProxy(RedisProxyConfig{
		ListenerName:   "test-ping",
		ConnectTimeout: 2 * time.Second,
		ReadTimeout:    2 * time.Second,
		WriteTimeout:   2 * time.Second,
		Backends:       backends,
		BackendName:    "test-redis",
	}, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create a pipe to simulate client connection
	clientConn, serverConn := net.Pipe()

	// Handle connection in background
	done := make(chan struct{})
	go func() {
		defer close(done)
		proxy.HandleConnection(ctx, serverConn)
	}()

	// Send PING command as RESP
	pingCmd := EncodeCommand("PING")
	_, err := clientConn.Write(pingCmd)
	if err != nil {
		t.Fatalf("Failed to write PING: %v", err)
	}

	// Read PONG response
	if err := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("Failed to set read deadline: %v", err)
	}

	buf := make([]byte, 1024)
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatalf("Failed to read PONG response: %v", err)
	}

	response := string(buf[:n])
	if response != "+PONG\r\n" {
		t.Errorf("Expected '+PONG\\r\\n', got %q", response)
	}

	_ = clientConn.Close()
	<-done
}

func TestRedisProxy_HandleConnection_SetGet(t *testing.T) {
	logger := zaptest.NewLogger(t)

	backendListener, backendAddr := mockRedisBackend(t)
	defer func() { _ = backendListener.Close() }()

	host, portStr, _ := net.SplitHostPort(backendAddr)
	port := parseBackendPort(t, portStr)

	backends := []*pb.Endpoint{
		{Address: host, Port: port, Ready: true},
	}

	proxy := NewRedisProxy(RedisProxyConfig{
		ListenerName:   "test-setget",
		ConnectTimeout: 2 * time.Second,
		ReadTimeout:    2 * time.Second,
		WriteTimeout:   2 * time.Second,
		Backends:       backends,
		BackendName:    "test-redis",
	}, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientConn, serverConn := net.Pipe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		proxy.HandleConnection(ctx, serverConn)
	}()

	// Send SET command
	setCmd := EncodeCommand("SET", "mykey", "myvalue")
	_, err := clientConn.Write(setCmd)
	if err != nil {
		t.Fatalf("Failed to write SET: %v", err)
	}

	if err := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("Failed to set read deadline: %v", err)
	}

	buf := make([]byte, 1024)
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatalf("Failed to read SET response: %v", err)
	}

	if string(buf[:n]) != testRedisOK {
		t.Errorf("Expected '+OK\\r\\n', got %q", string(buf[:n]))
	}

	// Send GET command for existing key
	getCmd := EncodeCommand("GET", "existing")
	_, err = clientConn.Write(getCmd)
	if err != nil {
		t.Fatalf("Failed to write GET: %v", err)
	}

	if err := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("Failed to set read deadline: %v", err)
	}

	n, err = clientConn.Read(buf)
	if err != nil {
		t.Fatalf("Failed to read GET response: %v", err)
	}

	if string(buf[:n]) != "$5\r\nvalue\r\n" {
		t.Errorf("Expected '$5\\r\\nvalue\\r\\n', got %q", string(buf[:n]))
	}

	_ = clientConn.Close()
	<-done
}

func TestRedisProxy_HandleConnection_ErrorResponse(t *testing.T) {
	logger := zaptest.NewLogger(t)

	backendListener, backendAddr := mockRedisBackend(t)
	defer func() { _ = backendListener.Close() }()

	host, portStr, _ := net.SplitHostPort(backendAddr)
	port := parseBackendPort(t, portStr)

	backends := []*pb.Endpoint{
		{Address: host, Port: port, Ready: true},
	}

	proxy := NewRedisProxy(RedisProxyConfig{
		ListenerName:   "test-error",
		ConnectTimeout: 2 * time.Second,
		ReadTimeout:    2 * time.Second,
		WriteTimeout:   2 * time.Second,
		Backends:       backends,
		BackendName:    "test-redis",
	}, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientConn, serverConn := net.Pipe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		proxy.HandleConnection(ctx, serverConn)
	}()

	// Send an unknown command
	unknownCmd := EncodeCommand("FOOBAR")
	_, err := clientConn.Write(unknownCmd)
	if err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	if err := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("Failed to set read deadline: %v", err)
	}

	buf := make([]byte, 1024)
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatalf("Failed to read error response: %v", err)
	}

	response := string(buf[:n])
	if !strings.HasPrefix(response, "-ERR") {
		t.Errorf("Expected error response starting with '-ERR', got %q", response)
	}

	_ = clientConn.Close()
	<-done
}

func TestRedisProxy_HandleConnection_NoBackend(t *testing.T) {
	logger := zaptest.NewLogger(t)

	proxy := NewRedisProxy(RedisProxyConfig{
		ListenerName: "test-no-backend",
		Backends:     []*pb.Endpoint{},
		BackendName:  "test-redis",
		ReadTimeout:  1 * time.Second,
		WriteTimeout: 1 * time.Second,
	}, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientConn, serverConn := net.Pipe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		proxy.HandleConnection(ctx, serverConn)
	}()

	// Should receive an error response
	if err := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("Failed to set read deadline: %v", err)
	}

	buf := make([]byte, 1024)
	n, err := clientConn.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Failed to read: %v", err)
	}

	if n > 0 {
		response := string(buf[:n])
		if !strings.HasPrefix(response, "-ERR") {
			t.Errorf("Expected error response, got %q", response)
		}
	}

	_ = clientConn.Close()
	<-done
}

func TestRedisProxy_HandleConnection_Quit(t *testing.T) {
	logger := zaptest.NewLogger(t)

	backendListener, backendAddr := mockRedisBackend(t)
	defer func() { _ = backendListener.Close() }()

	host, portStr, _ := net.SplitHostPort(backendAddr)
	port := parseBackendPort(t, portStr)

	backends := []*pb.Endpoint{
		{Address: host, Port: port, Ready: true},
	}

	proxy := NewRedisProxy(RedisProxyConfig{
		ListenerName:   "test-quit",
		ConnectTimeout: 2 * time.Second,
		ReadTimeout:    2 * time.Second,
		WriteTimeout:   2 * time.Second,
		Backends:       backends,
		BackendName:    "test-redis",
	}, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientConn, serverConn := net.Pipe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		proxy.HandleConnection(ctx, serverConn)
	}()

	// Send QUIT command
	quitCmd := EncodeCommand("QUIT")
	_, err := clientConn.Write(quitCmd)
	if err != nil {
		t.Fatalf("Failed to write QUIT: %v", err)
	}

	if err := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("Failed to set read deadline: %v", err)
	}

	buf := make([]byte, 1024)
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatalf("Failed to read QUIT response: %v", err)
	}

	if string(buf[:n]) != testRedisOK {
		t.Errorf("Expected '+OK\\r\\n', got %q", string(buf[:n]))
	}

	// Connection should be closed by proxy after QUIT
	select {
	case <-done:
		// Expected
	case <-time.After(2 * time.Second):
		t.Error("Proxy did not close connection after QUIT")
	}

	_ = clientConn.Close()
}

func TestRedisProxy_Draining_RejectsNew(t *testing.T) {
	logger := zaptest.NewLogger(t)

	proxy := NewRedisProxy(RedisProxyConfig{
		ListenerName: "test-draining",
		Backends:     []*pb.Endpoint{{Address: "10.0.0.1", Port: 6379, Ready: true}},
		BackendName:  "test-redis",
	}, logger)

	proxy.draining.Store(true)

	clientConn, serverConn := net.Pipe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		proxy.HandleConnection(context.Background(), serverConn)
	}()

	// The server side should be closed
	select {
	case <-done:
		// Good - connection was rejected
	case <-time.After(1 * time.Second):
		t.Error("Expected connection to be rejected while draining")
	}

	_ = clientConn.Close()
}

// --- Redis Health Check Tests ---

func TestRedisHealthChecker_PingPong(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Start mock Redis backend
	backendListener, backendAddr := mockRedisBackend(t)
	defer func() { _ = backendListener.Close() }()

	checker := NewRedisHealthChecker(RedisHealthCheckerConfig{
		CheckTimeout: 2 * time.Second,
	}, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	healthy := checker.CheckSingle(ctx, backendAddr)
	if !healthy {
		t.Error("Expected healthy backend with PONG response")
	}
}

func TestRedisHealthChecker_UnreachableBackend(t *testing.T) {
	logger := zaptest.NewLogger(t)

	checker := NewRedisHealthChecker(RedisHealthCheckerConfig{
		CheckTimeout: 500 * time.Millisecond,
	}, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Use an address that should fail to connect
	healthy := checker.CheckSingle(ctx, "127.0.0.1:1")
	if healthy {
		t.Error("Expected unhealthy for unreachable backend")
	}
}

func TestRedisHealthChecker_DefaultConfig(t *testing.T) {
	logger := zap.NewNop()

	checker := NewRedisHealthChecker(RedisHealthCheckerConfig{}, logger)

	if checker.config.CheckInterval != DefaultRedisHealthCheckInterval {
		t.Errorf("Expected default check interval %v, got %v",
			DefaultRedisHealthCheckInterval, checker.config.CheckInterval)
	}
	if checker.config.CheckTimeout != DefaultRedisHealthCheckTimeout {
		t.Errorf("Expected default check timeout %v, got %v",
			DefaultRedisHealthCheckTimeout, checker.config.CheckTimeout)
	}
	if checker.config.FailureThreshold != DefaultRedisHealthCheckThreshold {
		t.Errorf("Expected default failure threshold %d, got %d",
			DefaultRedisHealthCheckThreshold, checker.config.FailureThreshold)
	}
}

func TestRedisHealthChecker_UpdateBackends(t *testing.T) {
	logger := zaptest.NewLogger(t)

	checker := NewRedisHealthChecker(RedisHealthCheckerConfig{}, logger)

	backends := []*pb.Endpoint{
		{Address: "10.0.0.1", Port: 6379, Ready: true},
		{Address: testRedisAddr2, Port: 6379, Ready: true},
	}
	checker.UpdateBackends(backends)

	result := checker.GetBackends()
	if len(result) != 2 {
		t.Errorf("Expected 2 backends, got %d", len(result))
	}
}

// --- Connection Pool Tests ---

func TestRedisConnPool_GetPut(t *testing.T) {
	pool := &redisConnPool{
		addr:     "127.0.0.1:6379",
		conns:    make([]*redisBackendConn, 0, 5),
		maxSize:  5,
		timeout:  5 * time.Second,
		idleTime: 5 * time.Minute,
	}

	// Pool should be empty initially
	conn := pool.get()
	if conn != nil {
		t.Error("Expected nil from empty pool")
	}

	// Create a pipe to simulate a connection
	clientConn, serverConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()

	// Put a connection
	ok := pool.put(serverConn, "127.0.0.1:6379")
	if !ok {
		t.Error("Expected put to succeed")
	}

	// Get should return the connection
	got := pool.get()
	if got == nil {
		t.Error("Expected connection from pool")
	}

	// Pool should be empty again
	got = pool.get()
	if got != nil {
		t.Error("Expected nil after getting the only connection")
	}
}

func TestRedisConnPool_MaxSize(t *testing.T) {
	pool := &redisConnPool{
		addr:     "127.0.0.1:6379",
		conns:    make([]*redisBackendConn, 0, 2),
		maxSize:  2,
		timeout:  5 * time.Second,
		idleTime: 5 * time.Minute,
	}

	// Fill the pool
	for i := 0; i < 2; i++ {
		c, s := net.Pipe()
		defer func() { _ = c.Close() }()
		ok := pool.put(s, "127.0.0.1:6379")
		if !ok {
			t.Fatalf("Expected put %d to succeed", i)
		}
	}

	// Third put should fail
	c, s := net.Pipe()
	defer func() { _ = c.Close() }()
	ok := pool.put(s, "127.0.0.1:6379")
	if ok {
		t.Error("Expected put to fail when pool is full")
	}
	_ = s.Close()
}

func TestRedisConnPool_IdleExpiry(t *testing.T) {
	pool := &redisConnPool{
		addr:     "127.0.0.1:6379",
		conns:    make([]*redisBackendConn, 0, 5),
		maxSize:  5,
		timeout:  5 * time.Second,
		idleTime: 50 * time.Millisecond, // Very short idle time for testing
	}

	c, s := net.Pipe()
	defer func() { _ = c.Close() }()

	ok := pool.put(s, "127.0.0.1:6379")
	if !ok {
		t.Fatal("Expected put to succeed")
	}

	// Wait for the connection to expire
	time.Sleep(100 * time.Millisecond)

	// Get should return nil because the connection expired
	got := pool.get()
	if got != nil {
		t.Error("Expected nil for expired connection")
		_ = got.Close()
	}
}

func TestRedisConnPool_Close(t *testing.T) {
	pool := &redisConnPool{
		addr:     "127.0.0.1:6379",
		conns:    make([]*redisBackendConn, 0, 5),
		maxSize:  5,
		timeout:  5 * time.Second,
		idleTime: 5 * time.Minute,
	}

	for i := 0; i < 3; i++ {
		c, s := net.Pipe()
		defer func() { _ = c.Close() }()
		_ = pool.put(s, "127.0.0.1:6379")
	}

	pool.close()

	if len(pool.conns) != 0 {
		t.Errorf("Expected empty pool after close, got %d connections", len(pool.conns))
	}
}

// --- isConnectionClosed Tests ---

func TestIsConnectionClosed(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"EOF", io.EOF, true},
		{"wrapped EOF", fmt.Errorf("read: %w", io.EOF), true},
		{"connection reset", fmt.Errorf("read: connection reset by peer"), true},
		{"closed", fmt.Errorf("use of closed network connection"), true},
		{"other error", fmt.Errorf("some other error"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isConnectionClosed(tc.err)
			if result != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}

// --- RESP Protocol Edge Cases ---

func TestRESPReader_InvalidType(t *testing.T) {
	input := "XINVALID\r\n"
	reader := NewRESPReader(strings.NewReader(input))

	_, err := reader.ReadValue()
	if err == nil {
		t.Error("Expected error for invalid RESP type")
	}
}

func TestRESPReader_EmptyInput(t *testing.T) {
	reader := NewRESPReader(strings.NewReader(""))

	_, err := reader.ReadValue()
	if err == nil {
		t.Error("Expected error for empty input")
	}
}

func TestRESPReader_TruncatedBulkString(t *testing.T) {
	// Bulk string declares 10 bytes but only 5 are available
	input := "$10\r\nhello"
	reader := NewRESPReader(strings.NewReader(input))

	_, err := reader.ReadValue()
	if err == nil {
		t.Error("Expected error for truncated bulk string")
	}
}

func TestRESPReader_NestedArray(t *testing.T) {
	// Array containing another array
	input := "*2\r\n*2\r\n$1\r\na\r\n$1\r\nb\r\n$1\r\nc\r\n"
	reader := NewRESPReader(strings.NewReader(input))

	val, err := reader.ReadValue()
	if err != nil {
		t.Fatalf("Failed to read nested array: %v", err)
	}

	if val.Type != RESPTypeArray || len(val.Array) != 2 {
		t.Fatalf("Expected 2-element array, got type=%c len=%d", byte(val.Type), len(val.Array))
	}

	// First element should be a sub-array
	sub := val.Array[0]
	if sub.Type != RESPTypeArray || len(sub.Array) != 2 {
		t.Errorf("Expected nested 2-element array, got type=%c len=%d", byte(sub.Type), len(sub.Array))
	}
	if sub.Array[0].Str != "a" || sub.Array[1].Str != "b" {
		t.Errorf("Expected [a, b], got [%s, %s]", sub.Array[0].Str, sub.Array[1].Str)
	}

	// Second element should be bulk string "c"
	if val.Array[1].Str != "c" {
		t.Errorf("Expected 'c', got %q", val.Array[1].Str)
	}
}
