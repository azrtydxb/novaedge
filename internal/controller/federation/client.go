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

package federation

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// PeerClient manages the connection to a federation peer
type PeerClient struct {
	peer   *PeerInfo
	config *FederationConfig
	logger *zap.Logger

	// Connection management
	conn   *grpc.ClientConn
	client pb.FederationServiceClient
	connMu sync.RWMutex

	// Stream for bidirectional sync
	stream     pb.FederationService_SyncStreamClient
	streamMu   sync.Mutex
	streamCtx  context.Context
	streamCancel context.CancelFunc

	// Callbacks
	onMessage func(*pb.SyncMessage)
	onDisconnect func()

	// State
	connected bool
	healthy   bool
	stateMu   sync.RWMutex

	// Metrics
	lastPing   time.Time
	pingLatency time.Duration
}

// NewPeerClient creates a new peer client
func NewPeerClient(peer *PeerInfo, config *FederationConfig, logger *zap.Logger) *PeerClient {
	return &PeerClient{
		peer:   peer,
		config: config,
		logger: logger.Named("peer").With(zap.String("peer", peer.Name)),
	}
}

// Connect establishes a connection to the peer
func (c *PeerClient) Connect(ctx context.Context) error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn != nil {
		return nil // Already connected
	}

	opts := []grpc.DialOption{
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             5 * time.Second,
			PermitWithoutStream: true,
		}),
	}

	// Configure TLS if enabled
	if c.peer.TLSEnabled {
		tlsConfig, err := c.buildTLSConfig()
		if err != nil {
			return fmt.Errorf("failed to build TLS config: %w", err)
		}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	c.logger.Info("Connecting to peer", zap.String("endpoint", c.peer.Endpoint))

	conn, err := grpc.NewClient(c.peer.Endpoint, opts...)
	if err != nil {
		return fmt.Errorf("failed to dial peer: %w", err)
	}

	c.conn = conn
	c.client = pb.NewFederationServiceClient(conn)

	c.setConnected(true)
	c.logger.Info("Connected to peer")

	return nil
}

// Disconnect closes the connection to the peer
func (c *PeerClient) Disconnect() {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.streamCancel != nil {
		c.streamCancel()
		c.streamCancel = nil
	}

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
		c.client = nil
	}

	c.setConnected(false)
	c.setHealthy(false)

	c.logger.Info("Disconnected from peer")
}

// StartSyncStream establishes a bidirectional sync stream
func (c *PeerClient) StartSyncStream(ctx context.Context, localVectorClock map[string]int64) error {
	c.streamMu.Lock()
	defer c.streamMu.Unlock()

	c.connMu.RLock()
	client := c.client
	c.connMu.RUnlock()

	if client == nil {
		return fmt.Errorf("not connected to peer")
	}

	streamCtx, cancel := context.WithCancel(ctx)
	c.streamCtx = streamCtx
	c.streamCancel = cancel

	stream, err := client.SyncStream(streamCtx)
	if err != nil {
		cancel()
		return fmt.Errorf("failed to create sync stream: %w", err)
	}

	c.stream = stream

	// Send handshake
	handshake := &pb.SyncMessage{
		Message: &pb.SyncMessage_Handshake{
			Handshake: &pb.SyncHandshake{
				FederationId:    c.config.FederationID,
				MemberName:      c.config.LocalMember.Name,
				Region:          c.config.LocalMember.Region,
				Zone:            c.config.LocalMember.Zone,
				VectorClock:     localVectorClock,
				ProtocolVersion: ProtocolVersion,
				Compression:     c.config.CompressionEnabled,
			},
		},
	}

	if err := stream.Send(handshake); err != nil {
		cancel()
		return fmt.Errorf("failed to send handshake: %w", err)
	}

	// Wait for handshake response
	msg, err := stream.Recv()
	if err != nil {
		cancel()
		return fmt.Errorf("failed to receive handshake: %w", err)
	}

	if msg.GetHandshake() == nil {
		cancel()
		return fmt.Errorf("expected handshake response, got %T", msg.Message)
	}

	c.logger.Info("Sync stream established with peer")
	c.setHealthy(true)

	// Start receive goroutine
	go c.receiveLoop(streamCtx)

	return nil
}

// receiveLoop receives messages from the peer
func (c *PeerClient) receiveLoop(ctx context.Context) {
	defer func() {
		c.setConnected(false)
		if c.onDisconnect != nil {
			c.onDisconnect()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.streamMu.Lock()
		stream := c.stream
		c.streamMu.Unlock()

		if stream == nil {
			return
		}

		msg, err := stream.Recv()
		if err != nil {
			c.logger.Error("Error receiving from peer", zap.Error(err))
			return
		}

		if c.onMessage != nil {
			c.onMessage(msg)
		}
	}
}

// SendMessage sends a message to the peer
func (c *PeerClient) SendMessage(msg *pb.SyncMessage) error {
	c.streamMu.Lock()
	defer c.streamMu.Unlock()

	if c.stream == nil {
		return fmt.Errorf("sync stream not established")
	}

	return c.stream.Send(msg)
}

// SendChange sends a resource change to the peer
func (c *PeerClient) SendChange(change *pb.ResourceChange) error {
	return c.SendMessage(&pb.SyncMessage{
		Message: &pb.SyncMessage_Change{
			Change: change,
		},
	})
}

// SendHeartbeat sends a heartbeat to the peer
func (c *PeerClient) SendHeartbeat(vectorClock map[string]int64, pendingChanges, agentCount int32) error {
	return c.SendMessage(&pb.SyncMessage{
		Message: &pb.SyncMessage_Heartbeat{
			Heartbeat: &pb.Heartbeat{
				VectorClock:    vectorClock,
				Timestamp:      time.Now().UnixNano(),
				PendingChanges: pendingChanges,
				AgentCount:     agentCount,
			},
		},
	})
}

// SendAck sends an acknowledgment to the peer
func (c *PeerClient) SendAck(changeIDs []string, vectorClock map[string]int64, errors []*pb.ChangeError) error {
	return c.SendMessage(&pb.SyncMessage{
		Message: &pb.SyncMessage_Ack{
			Ack: &pb.SyncAck{
				ChangeIds:   changeIDs,
				VectorClock: vectorClock,
				Errors:      errors,
			},
		},
	})
}

// Ping sends a ping to the peer and measures latency
func (c *PeerClient) Ping(ctx context.Context) (time.Duration, error) {
	c.connMu.RLock()
	client := c.client
	c.connMu.RUnlock()

	if client == nil {
		return 0, fmt.Errorf("not connected to peer")
	}

	start := time.Now()
	_, err := client.Ping(ctx, &pb.PingRequest{
		FederationId: c.config.FederationID,
		Sender:       c.config.LocalMember.Name,
		Timestamp:    start.UnixNano(),
	})
	latency := time.Since(start)

	if err != nil {
		c.setHealthy(false)
		return latency, err
	}

	c.stateMu.Lock()
	c.lastPing = time.Now()
	c.pingLatency = latency
	c.stateMu.Unlock()

	c.setHealthy(true)
	return latency, nil
}

// GetState retrieves the current state of the peer
func (c *PeerClient) GetState(ctx context.Context) (*pb.GetStateResponse, error) {
	c.connMu.RLock()
	client := c.client
	c.connMu.RUnlock()

	if client == nil {
		return nil, fmt.Errorf("not connected to peer")
	}

	return client.GetState(ctx, &pb.GetStateRequest{
		FederationId:    c.config.FederationID,
		RequesterMember: c.config.LocalMember.Name,
	})
}

// RequestFullSync requests a full sync from the peer
func (c *PeerClient) RequestFullSync(ctx context.Context, resourceTypes, namespaces []string, vectorClock map[string]int64) ([]*pb.ResourceBatch, error) {
	c.connMu.RLock()
	client := c.client
	c.connMu.RUnlock()

	if client == nil {
		return nil, fmt.Errorf("not connected to peer")
	}

	stream, err := client.RequestFullSync(ctx, &pb.FullSyncRequest{
		FederationId:    c.config.FederationID,
		RequesterMember: c.config.LocalMember.Name,
		ResourceTypes:   resourceTypes,
		Namespaces:      namespaces,
		VectorClock:     vectorClock,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to request full sync: %w", err)
	}

	var batches []*pb.ResourceBatch
	for {
		batch, err := stream.Recv()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return batches, err
		}
		batches = append(batches, batch)
		if batch.IsLast {
			break
		}
	}

	return batches, nil
}

// OnMessage sets the callback for incoming messages
func (c *PeerClient) OnMessage(fn func(*pb.SyncMessage)) {
	c.onMessage = fn
}

// OnDisconnect sets the callback for disconnection
func (c *PeerClient) OnDisconnect(fn func()) {
	c.onDisconnect = fn
}

// IsConnected returns whether the client is connected
func (c *PeerClient) IsConnected() bool {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.connected
}

// IsHealthy returns whether the peer is healthy
func (c *PeerClient) IsHealthy() bool {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.healthy
}

// GetLatency returns the last measured ping latency
func (c *PeerClient) GetLatency() time.Duration {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.pingLatency
}

// setConnected sets the connected state
func (c *PeerClient) setConnected(connected bool) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.connected = connected
}

// setHealthy sets the healthy state
func (c *PeerClient) setHealthy(healthy bool) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.healthy = healthy
}

// buildTLSConfig builds the TLS configuration for the peer connection
func (c *PeerClient) buildTLSConfig() (*tls.Config, error) {
	config := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: c.peer.InsecureSkipVerify,
	}

	if c.peer.TLSServerName != "" {
		config.ServerName = c.peer.TLSServerName
	}

	// Add CA certificate from PeerInfo if available
	if len(c.peer.CACert) > 0 {
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(c.peer.CACert) {
			return nil, fmt.Errorf("failed to parse CA certificate for peer %s", c.peer.Name)
		}
		config.RootCAs = certPool
		c.logger.Debug("Loaded CA certificate for peer",
			zap.String("peer", c.peer.Name),
		)
	}

	// Add client certificate for mTLS from PeerInfo if available
	if len(c.peer.ClientCert) > 0 && len(c.peer.ClientKey) > 0 {
		cert, err := tls.X509KeyPair(c.peer.ClientCert, c.peer.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate for peer %s: %w", c.peer.Name, err)
		}
		config.Certificates = []tls.Certificate{cert}
		c.logger.Debug("Loaded client certificate for peer",
			zap.String("peer", c.peer.Name),
		)
	}

	return config, nil
}

// PeerClientWithCerts adds certificate support to the peer client
type PeerClientWithCerts struct {
	*PeerClient
	caCert     []byte
	clientCert []byte
	clientKey  []byte
}

// NewPeerClientWithCerts creates a peer client with TLS certificates
func NewPeerClientWithCerts(peer *PeerInfo, config *FederationConfig, logger *zap.Logger, caCert, clientCert, clientKey []byte) *PeerClientWithCerts {
	return &PeerClientWithCerts{
		PeerClient: NewPeerClient(peer, config, logger),
		caCert:     caCert,
		clientCert: clientCert,
		clientKey:  clientKey,
	}
}

// buildTLSConfig builds the TLS configuration with certificates
func (c *PeerClientWithCerts) buildTLSConfig() (*tls.Config, error) {
	config := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: c.peer.InsecureSkipVerify,
	}

	if c.peer.TLSServerName != "" {
		config.ServerName = c.peer.TLSServerName
	}

	// Add CA certificate
	if len(c.caCert) > 0 {
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(c.caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		config.RootCAs = certPool
	}

	// Add client certificate for mTLS
	if len(c.clientCert) > 0 && len(c.clientKey) > 0 {
		cert, err := tls.X509KeyPair(c.clientCert, c.clientKey)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		config.Certificates = []tls.Certificate{cert}
	}

	return config, nil
}
