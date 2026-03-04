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

// Package sdwan provides SD-WAN functionality for intelligent path selection,
// DSCP quality-of-service marking, and WAN link management.
package sdwan

import "syscall"

// dscpClassMap maps DSCP class names to their numeric Per-Hop Behavior
// (PHB) values as defined in RFC 2474 / RFC 2597 / RFC 3246.
var dscpClassMap = map[string]int{
	"EF":      46, // Expedited Forwarding (voice/real-time)
	"AF41":    34, // Assured Forwarding class 4, low drop
	"AF42":    36, // Assured Forwarding class 4, medium drop
	"AF43":    38, // Assured Forwarding class 4, high drop
	"AF31":    26, // Assured Forwarding class 3, low drop
	"AF32":    28, // Assured Forwarding class 3, medium drop
	"AF33":    30, // Assured Forwarding class 3, high drop
	"AF21":    18, // Assured Forwarding class 2, low drop (transactional data)
	"AF22":    20, // Assured Forwarding class 2, medium drop
	"AF23":    22, // Assured Forwarding class 2, high drop
	"AF11":    10, // Assured Forwarding class 1, low drop
	"AF12":    12, // Assured Forwarding class 1, medium drop
	"AF13":    14, // Assured Forwarding class 1, high drop
	"CS7":     56, // Network Control
	"CS6":     48, // Internetwork Control
	"CS5":     40, // VOICE-ADMIT / signaling
	"CS4":     32, // Real-Time Interactive
	"CS3":     24, // Broadcast Video
	"CS2":     16, // OAM
	"CS1":     8,  // Scavenger / bulk data
	"BE":      0,  // Best Effort
	"default": 0,  // Best Effort
}

// DSCPClassToValue maps DSCP class names to their numeric Per-Hop Behavior
// (PHB) values as defined in RFC 2474 / RFC 2597 / RFC 3246.
func DSCPClassToValue(class string) int {
	if v, ok := dscpClassMap[class]; ok {
		return v
	}
	return 0 // Best Effort default
}

// DSCPToTOS converts a 6-bit DSCP value to an 8-bit IP TOS byte by shifting
// left 2 bits (the ECN field occupies the lower 2 bits).
func DSCPToTOS(dscp int) int {
	return dscp << 2
}

// SetDSCPOnSocket sets the DSCP/TOS value on a raw file descriptor.
// If the DSCP class resolves to 0 (Best Effort), the call is a no-op.
func SetDSCPOnSocket(fd uintptr, dscpClass string) error {
	dscp := DSCPClassToValue(dscpClass)
	if dscp == 0 {
		return nil
	}
	tos := DSCPToTOS(dscp)
	return syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_TOS, tos) //nolint:gosec // G115: fd conversion is safe on supported 64-bit platforms
}
