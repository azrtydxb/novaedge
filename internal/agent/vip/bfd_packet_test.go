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
	"testing"
)

func TestEncodeBFDPacket_Basic(t *testing.T) {
	pkt := &bfdControlPacket{
		Version:               bfdVersion,
		Diagnostic:            bfdDiagNone,
		State:                 BFDStateUp,
		DetectMult:            3,
		MyDiscriminator:       42,
		YourDiscriminator:     99,
		DesiredMinTxInterval:  300000,
		RequiredMinRxInterval: 300000,
	}

	data, err := encodeBFDPacket(pkt)
	if err != nil {
		t.Fatalf("encodeBFDPacket failed: %v", err)
	}

	if len(data) != bfdPacketLength {
		t.Fatalf("expected %d bytes, got %d", bfdPacketLength, len(data))
	}

	// Verify version (bits 7-5 of byte 0) = 1
	version := (data[0] >> 5) & 0x07
	if version != 1 {
		t.Errorf("expected version 1, got %d", version)
	}

	// Verify diagnostic (bits 4-0 of byte 0) = 0
	diag := data[0] & 0x1F
	if diag != 0 {
		t.Errorf("expected diagnostic 0, got %d", diag)
	}

	// Verify state (bits 7-6 of byte 1) = 3 (Up)
	state := (data[1] >> 6) & 0x03
	if state != 3 {
		t.Errorf("expected state 3 (Up), got %d", state)
	}

	// Verify detect multiplier
	if data[2] != 3 {
		t.Errorf("expected detect mult 3, got %d", data[2])
	}

	// Verify length
	if data[3] != bfdPacketLength {
		t.Errorf("expected length %d, got %d", bfdPacketLength, data[3])
	}

	// Verify my discriminator
	myDiscr := binary.BigEndian.Uint32(data[4:8])
	if myDiscr != 42 {
		t.Errorf("expected my discriminator 42, got %d", myDiscr)
	}

	// Verify your discriminator
	yourDiscr := binary.BigEndian.Uint32(data[8:12])
	if yourDiscr != 99 {
		t.Errorf("expected your discriminator 99, got %d", yourDiscr)
	}

	// Verify desired min TX interval
	desiredMinTx := binary.BigEndian.Uint32(data[12:16])
	if desiredMinTx != 300000 {
		t.Errorf("expected desired min TX 300000, got %d", desiredMinTx)
	}

	// Verify required min RX interval
	requiredMinRx := binary.BigEndian.Uint32(data[16:20])
	if requiredMinRx != 300000 {
		t.Errorf("expected required min RX 300000, got %d", requiredMinRx)
	}

	// Verify required min echo RX interval = 0 (default)
	echoRx := binary.BigEndian.Uint32(data[20:24])
	if echoRx != 0 {
		t.Errorf("expected echo RX 0, got %d", echoRx)
	}
}

func TestEncodeBFDPacket_NilPacket(t *testing.T) {
	_, err := encodeBFDPacket(nil)
	if err == nil {
		t.Fatal("expected error for nil packet")
	}
}

func TestDecodeBFDPacket_TooShort(t *testing.T) {
	_, err := decodeBFDPacket(make([]byte, 10))
	if err == nil {
		t.Fatal("expected error for short packet")
	}
	if !errors.Is(err, errBFDPacketTooShort) {
		t.Errorf("expected errBFDPacketTooShort, got %v", err)
	}
}

func TestDecodeBFDPacket_InvalidVersion(t *testing.T) {
	data := make([]byte, bfdPacketLength)
	// Set version to 2 (invalid)
	data[0] = 2 << 5
	data[3] = bfdPacketLength

	_, err := decodeBFDPacket(data)
	if err == nil {
		t.Fatal("expected error for invalid version")
	}
	if !errors.Is(err, errBFDInvalidVersion) {
		t.Errorf("expected errBFDInvalidVersion, got %v", err)
	}
}

func TestDecodeBFDPacket_InvalidLength(t *testing.T) {
	data := make([]byte, bfdPacketLength)
	// Set version to 1
	data[0] = 1 << 5
	// Set length field larger than actual data
	data[3] = 48

	_, err := decodeBFDPacket(data)
	if err == nil {
		t.Fatal("expected error for mismatched length")
	}
	if !errors.Is(err, errBFDInvalidLength) {
		t.Errorf("expected errBFDInvalidLength, got %v", err)
	}
}

func TestBFDPacket_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		pkt  *bfdControlPacket
	}{
		{
			name: "AdminDown state",
			pkt: &bfdControlPacket{
				Version:               bfdVersion,
				Diagnostic:            bfdDiagAdminDown,
				State:                 BFDStateAdminDown,
				DetectMult:            3,
				MyDiscriminator:       1,
				YourDiscriminator:     0,
				DesiredMinTxInterval:  1000000,
				RequiredMinRxInterval: 1000000,
			},
		},
		{
			name: "Down state",
			pkt: &bfdControlPacket{
				Version:               bfdVersion,
				Diagnostic:            bfdDiagNone,
				State:                 BFDStateDown,
				DetectMult:            5,
				MyDiscriminator:       100,
				YourDiscriminator:     0,
				DesiredMinTxInterval:  300000,
				RequiredMinRxInterval: 300000,
			},
		},
		{
			name: "Init state",
			pkt: &bfdControlPacket{
				Version:               bfdVersion,
				Diagnostic:            bfdDiagNone,
				State:                 BFDStateInit,
				DetectMult:            3,
				MyDiscriminator:       200,
				YourDiscriminator:     100,
				DesiredMinTxInterval:  300000,
				RequiredMinRxInterval: 300000,
			},
		},
		{
			name: "Up state with all flags",
			pkt: &bfdControlPacket{
				Version:                   bfdVersion,
				Diagnostic:                bfdDiagNone,
				State:                     BFDStateUp,
				Poll:                      true,
				Final:                     true,
				ControlPlaneIndependent:   true,
				AuthPresent:               true,
				Demand:                    true,
				Multipoint:                true,
				DetectMult:                3,
				MyDiscriminator:           500,
				YourDiscriminator:         600,
				DesiredMinTxInterval:      300000,
				RequiredMinRxInterval:     300000,
				RequiredMinEchoRxInterval: 50000,
			},
		},
		{
			name: "Poll flag only",
			pkt: &bfdControlPacket{
				Version:               bfdVersion,
				State:                 BFDStateUp,
				Poll:                  true,
				DetectMult:            3,
				MyDiscriminator:       1,
				YourDiscriminator:     2,
				DesiredMinTxInterval:  300000,
				RequiredMinRxInterval: 300000,
			},
		},
		{
			name: "Final flag only",
			pkt: &bfdControlPacket{
				Version:               bfdVersion,
				State:                 BFDStateUp,
				Final:                 true,
				DetectMult:            3,
				MyDiscriminator:       1,
				YourDiscriminator:     2,
				DesiredMinTxInterval:  300000,
				RequiredMinRxInterval: 300000,
			},
		},
		{
			name: "Control plane independent flag only",
			pkt: &bfdControlPacket{
				Version:                 bfdVersion,
				State:                   BFDStateDown,
				ControlPlaneIndependent: true,
				DetectMult:              3,
				MyDiscriminator:         1,
				DesiredMinTxInterval:    300000,
				RequiredMinRxInterval:   300000,
			},
		},
		{
			name: "Large discriminator values",
			pkt: &bfdControlPacket{
				Version:                   bfdVersion,
				State:                     BFDStateUp,
				DetectMult:                255,
				MyDiscriminator:           0xFFFFFFFF,
				YourDiscriminator:         0xDEADBEEF,
				DesiredMinTxInterval:      0xFFFFFFFF,
				RequiredMinRxInterval:     0xFFFFFFFF,
				RequiredMinEchoRxInterval: 0xFFFFFFFF,
			},
		},
		{
			name: "Diagnostic - control detect expired",
			pkt: &bfdControlPacket{
				Version:               bfdVersion,
				Diagnostic:            bfdDiagControlDetectExpired,
				State:                 BFDStateDown,
				DetectMult:            3,
				MyDiscriminator:       10,
				DesiredMinTxInterval:  300000,
				RequiredMinRxInterval: 300000,
			},
		},
		{
			name: "Diagnostic - neighbor down",
			pkt: &bfdControlPacket{
				Version:               bfdVersion,
				Diagnostic:            bfdDiagNeighborDown,
				State:                 BFDStateDown,
				DetectMult:            3,
				MyDiscriminator:       10,
				DesiredMinTxInterval:  300000,
				RequiredMinRxInterval: 300000,
			},
		},
		{
			name: "Diagnostic - path down",
			pkt: &bfdControlPacket{
				Version:               bfdVersion,
				Diagnostic:            bfdDiagPathDown,
				State:                 BFDStateDown,
				DetectMult:            3,
				MyDiscriminator:       10,
				DesiredMinTxInterval:  300000,
				RequiredMinRxInterval: 300000,
			},
		},
		{
			name: "Diagnostic - forwarding reset",
			pkt: &bfdControlPacket{
				Version:               bfdVersion,
				Diagnostic:            bfdDiagForwardingReset,
				State:                 BFDStateDown,
				DetectMult:            3,
				MyDiscriminator:       10,
				DesiredMinTxInterval:  300000,
				RequiredMinRxInterval: 300000,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := encodeBFDPacket(tt.pkt)
			if err != nil {
				t.Fatalf("encodeBFDPacket failed: %v", err)
			}

			decoded, err := decodeBFDPacket(data)
			if err != nil {
				t.Fatalf("decodeBFDPacket failed: %v", err)
			}

			if decoded.Version != tt.pkt.Version {
				t.Errorf("Version: got %d, want %d", decoded.Version, tt.pkt.Version)
			}
			if decoded.Diagnostic != tt.pkt.Diagnostic {
				t.Errorf("Diagnostic: got %d, want %d", decoded.Diagnostic, tt.pkt.Diagnostic)
			}
			if decoded.State != tt.pkt.State {
				t.Errorf("State: got %v, want %v", decoded.State, tt.pkt.State)
			}
			if decoded.Poll != tt.pkt.Poll {
				t.Errorf("Poll: got %v, want %v", decoded.Poll, tt.pkt.Poll)
			}
			if decoded.Final != tt.pkt.Final {
				t.Errorf("Final: got %v, want %v", decoded.Final, tt.pkt.Final)
			}
			if decoded.ControlPlaneIndependent != tt.pkt.ControlPlaneIndependent {
				t.Errorf("ControlPlaneIndependent: got %v, want %v", decoded.ControlPlaneIndependent, tt.pkt.ControlPlaneIndependent)
			}
			if decoded.AuthPresent != tt.pkt.AuthPresent {
				t.Errorf("AuthPresent: got %v, want %v", decoded.AuthPresent, tt.pkt.AuthPresent)
			}
			if decoded.Demand != tt.pkt.Demand {
				t.Errorf("Demand: got %v, want %v", decoded.Demand, tt.pkt.Demand)
			}
			if decoded.Multipoint != tt.pkt.Multipoint {
				t.Errorf("Multipoint: got %v, want %v", decoded.Multipoint, tt.pkt.Multipoint)
			}
			if decoded.DetectMult != tt.pkt.DetectMult {
				t.Errorf("DetectMult: got %d, want %d", decoded.DetectMult, tt.pkt.DetectMult)
			}
			if decoded.MyDiscriminator != tt.pkt.MyDiscriminator {
				t.Errorf("MyDiscriminator: got %d, want %d", decoded.MyDiscriminator, tt.pkt.MyDiscriminator)
			}
			if decoded.YourDiscriminator != tt.pkt.YourDiscriminator {
				t.Errorf("YourDiscriminator: got %d, want %d", decoded.YourDiscriminator, tt.pkt.YourDiscriminator)
			}
			if decoded.DesiredMinTxInterval != tt.pkt.DesiredMinTxInterval {
				t.Errorf("DesiredMinTxInterval: got %d, want %d", decoded.DesiredMinTxInterval, tt.pkt.DesiredMinTxInterval)
			}
			if decoded.RequiredMinRxInterval != tt.pkt.RequiredMinRxInterval {
				t.Errorf("RequiredMinRxInterval: got %d, want %d", decoded.RequiredMinRxInterval, tt.pkt.RequiredMinRxInterval)
			}
			if decoded.RequiredMinEchoRxInterval != tt.pkt.RequiredMinEchoRxInterval {
				t.Errorf("RequiredMinEchoRxInterval: got %d, want %d", decoded.RequiredMinEchoRxInterval, tt.pkt.RequiredMinEchoRxInterval)
			}
		})
	}
}

func TestBFDPacket_ByteLevelEncoding(t *testing.T) {
	// Manually construct a packet and verify exact byte layout per RFC 5880
	pkt := &bfdControlPacket{
		Version:                   1,
		Diagnostic:                bfdDiagControlDetectExpired, // 1
		State:                     BFDStateUp,                  // 3
		Poll:                      true,
		Final:                     false,
		ControlPlaneIndependent:   false,
		AuthPresent:               false,
		Demand:                    false,
		Multipoint:                false,
		DetectMult:                3,
		MyDiscriminator:           0x00000001,
		YourDiscriminator:         0x00000002,
		DesiredMinTxInterval:      300000, // 0x000493E0
		RequiredMinRxInterval:     300000, // 0x000493E0
		RequiredMinEchoRxInterval: 0,
	}

	data, err := encodeBFDPacket(pkt)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	// Byte 0: version=1 (001 in bits 7-5) | diagnostic=1 (00001 in bits 4-0) = 0x21
	expectedByte0 := byte(0x21)
	if data[0] != expectedByte0 {
		t.Errorf("byte 0: got 0x%02X, want 0x%02X", data[0], expectedByte0)
	}

	// Byte 1: state=3 (11 in bits 7-6) | P=1 | F=0 | C=0 | A=0 | D=0 | M=0 = 0xE0
	expectedByte1 := byte(0xE0)
	if data[1] != expectedByte1 {
		t.Errorf("byte 1: got 0x%02X, want 0x%02X", data[1], expectedByte1)
	}

	// Byte 2: detect multiplier = 3
	if data[2] != 3 {
		t.Errorf("byte 2: got %d, want 3", data[2])
	}

	// Byte 3: length = 24
	if data[3] != 24 {
		t.Errorf("byte 3: got %d, want 24", data[3])
	}
}

func TestDecodeBFDPacket_EmptyData(t *testing.T) {
	_, err := decodeBFDPacket(nil)
	if err == nil {
		t.Fatal("expected error for nil data")
	}

	_, err = decodeBFDPacket([]byte{})
	if err == nil {
		t.Fatal("expected error for empty data")
	}
}

func TestBFDPacket_ExtraDataAfterPacket(t *testing.T) {
	// A valid packet with extra bytes appended should still decode correctly
	pkt := &bfdControlPacket{
		Version:               bfdVersion,
		State:                 BFDStateDown,
		DetectMult:            3,
		MyDiscriminator:       42,
		DesiredMinTxInterval:  300000,
		RequiredMinRxInterval: 300000,
	}

	data, err := encodeBFDPacket(pkt)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	// Append extra bytes
	dataWithExtra := make([]byte, len(data)+10)
	copy(dataWithExtra, data)

	decoded, err := decodeBFDPacket(dataWithExtra)
	if err != nil {
		t.Fatalf("decode with extra data failed: %v", err)
	}

	if decoded.MyDiscriminator != 42 {
		t.Errorf("MyDiscriminator: got %d, want 42", decoded.MyDiscriminator)
	}
}
