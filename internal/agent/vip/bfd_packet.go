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

package vip

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// BFD control packet constants per RFC 5880 Section 4.1
const (
	bfdPacketLength = 24 // Fixed header length without authentication
	bfdVersion      = 1  // BFD protocol version
)

// BFD diagnostic values per RFC 5880 Section 4.1
const (
	bfdDiagNone                 uint8 = 0
	bfdDiagControlDetectExpired uint8 = 1
	bfdDiagEchoFailed           uint8 = 2
	bfdDiagNeighborDown         uint8 = 3
	bfdDiagForwardingReset      uint8 = 4
	bfdDiagPathDown             uint8 = 5
	bfdDiagConcatPathDown       uint8 = 6
	bfdDiagAdminDown            uint8 = 7
)

// bfdControlPacket represents a BFD control packet per RFC 5880 Section 4.1.
//
// Packet layout (24 bytes):
//
//	Byte 0:  Version (3 bits) | Diagnostic (5 bits)
//	Byte 1:  State (2 bits) | P | F | C | A | D | M flags
//	Byte 2:  Detect Multiplier
//	Byte 3:  Length
//	Bytes 4-7:   My Discriminator (uint32 big-endian)
//	Bytes 8-11:  Your Discriminator (uint32 big-endian)
//	Bytes 12-15: Desired Min TX Interval (uint32 big-endian, microseconds)
//	Bytes 16-19: Required Min RX Interval (uint32 big-endian, microseconds)
//	Bytes 20-23: Required Min Echo RX Interval (uint32 big-endian, microseconds)
type bfdControlPacket struct {
	Version                   uint8
	Diagnostic                uint8
	State                     BFDSessionState
	Poll                      bool
	Final                     bool
	ControlPlaneIndependent   bool
	AuthPresent               bool
	Demand                    bool
	Multipoint                bool
	DetectMult                uint8
	MyDiscriminator           uint32
	YourDiscriminator         uint32
	DesiredMinTxInterval      uint32 // microseconds
	RequiredMinRxInterval     uint32 // microseconds
	RequiredMinEchoRxInterval uint32 // microseconds
}

var (
	errBFDPacketTooShort = errors.New("BFD packet too short: minimum 24 bytes required")
	errBFDInvalidVersion = errors.New("BFD packet has invalid version: expected 1")
	errBFDInvalidLength  = errors.New("BFD packet length field does not match actual length")
)

// encodeBFDPacket encodes a BFD control packet into its wire format per RFC 5880.
func encodeBFDPacket(pkt *bfdControlPacket) ([]byte, error) {
	if pkt == nil {
		return nil, errors.New("cannot encode nil BFD packet")
	}

	buf := make([]byte, bfdPacketLength)

	// Byte 0: Version (3 bits) | Diagnostic (5 bits)
	buf[0] = (pkt.Version & 0x07 << 5) | (pkt.Diagnostic & 0x1F)

	// Byte 1: State (2 bits) | P | F | C | A | D | M flags
	buf[1] = clampInt32ToUint8(int32(pkt.State)&0x03) << 6
	if pkt.Poll {
		buf[1] |= 0x20
	}
	if pkt.Final {
		buf[1] |= 0x10
	}
	if pkt.ControlPlaneIndependent {
		buf[1] |= 0x08
	}
	if pkt.AuthPresent {
		buf[1] |= 0x04
	}
	if pkt.Demand {
		buf[1] |= 0x02
	}
	if pkt.Multipoint {
		buf[1] |= 0x01
	}

	// Byte 2: Detect Multiplier
	buf[2] = pkt.DetectMult

	// Byte 3: Length
	buf[3] = bfdPacketLength

	// Bytes 4-7: My Discriminator
	binary.BigEndian.PutUint32(buf[4:8], pkt.MyDiscriminator)

	// Bytes 8-11: Your Discriminator
	binary.BigEndian.PutUint32(buf[8:12], pkt.YourDiscriminator)

	// Bytes 12-15: Desired Min TX Interval
	binary.BigEndian.PutUint32(buf[12:16], pkt.DesiredMinTxInterval)

	// Bytes 16-19: Required Min RX Interval
	binary.BigEndian.PutUint32(buf[16:20], pkt.RequiredMinRxInterval)

	// Bytes 20-23: Required Min Echo RX Interval
	binary.BigEndian.PutUint32(buf[20:24], pkt.RequiredMinEchoRxInterval)

	return buf, nil
}

// decodeBFDPacket decodes a BFD control packet from its wire format per RFC 5880.
func decodeBFDPacket(data []byte) (*bfdControlPacket, error) {
	if len(data) < bfdPacketLength {
		return nil, fmt.Errorf("%w: got %d bytes", errBFDPacketTooShort, len(data))
	}

	pkt := &bfdControlPacket{}

	// Byte 0: Version (3 bits) | Diagnostic (5 bits)
	pkt.Version = (data[0] >> 5) & 0x07
	pkt.Diagnostic = data[0] & 0x1F

	if pkt.Version != bfdVersion {
		return nil, fmt.Errorf("%w: got %d", errBFDInvalidVersion, pkt.Version)
	}

	// Byte 1: State (2 bits) | P | F | C | A | D | M flags
	pkt.State = BFDSessionState((data[1] >> 6) & 0x03)
	pkt.Poll = data[1]&0x20 != 0
	pkt.Final = data[1]&0x10 != 0
	pkt.ControlPlaneIndependent = data[1]&0x08 != 0
	pkt.AuthPresent = data[1]&0x04 != 0
	pkt.Demand = data[1]&0x02 != 0
	pkt.Multipoint = data[1]&0x01 != 0

	// Byte 2: Detect Multiplier
	pkt.DetectMult = data[2]

	// Byte 3: Length validation
	pktLen := data[3]
	if int(pktLen) > len(data) {
		return nil, fmt.Errorf("%w: header says %d but got %d bytes", errBFDInvalidLength, pktLen, len(data))
	}

	// Bytes 4-7: My Discriminator
	pkt.MyDiscriminator = binary.BigEndian.Uint32(data[4:8])

	// Bytes 8-11: Your Discriminator
	pkt.YourDiscriminator = binary.BigEndian.Uint32(data[8:12])

	// Bytes 12-15: Desired Min TX Interval
	pkt.DesiredMinTxInterval = binary.BigEndian.Uint32(data[12:16])

	// Bytes 16-19: Required Min RX Interval
	pkt.RequiredMinRxInterval = binary.BigEndian.Uint32(data[16:20])

	// Bytes 20-23: Required Min Echo RX Interval
	pkt.RequiredMinEchoRxInterval = binary.BigEndian.Uint32(data[20:24])

	return pkt, nil
}

// clampInt32ToUint8 safely converts an int32 to uint8, clamping to
// [0, 255] to avoid integer overflow (gosec G115).
func clampInt32ToUint8(v int32) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}
