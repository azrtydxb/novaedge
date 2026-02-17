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

// DSCPClassToValue maps DSCP class names to their numeric Per-Hop Behavior
// (PHB) values as defined in RFC 2474 / RFC 2597 / RFC 3246.
func DSCPClassToValue(class string) int {
	switch class {
	case "EF":
		return 46 // Expedited Forwarding (voice/real-time)
	case "AF41":
		return 34 // Assured Forwarding class 4, low drop
	case "AF42":
		return 36 // Assured Forwarding class 4, medium drop
	case "AF43":
		return 38 // Assured Forwarding class 4, high drop
	case "AF31":
		return 26 // Assured Forwarding class 3, low drop
	case "AF32":
		return 28 // Assured Forwarding class 3, medium drop
	case "AF33":
		return 30 // Assured Forwarding class 3, high drop
	case "AF21":
		return 18 // Assured Forwarding class 2, low drop (transactional data)
	case "AF22":
		return 20 // Assured Forwarding class 2, medium drop
	case "AF23":
		return 22 // Assured Forwarding class 2, high drop
	case "AF11":
		return 10 // Assured Forwarding class 1, low drop
	case "AF12":
		return 12 // Assured Forwarding class 1, medium drop
	case "AF13":
		return 14 // Assured Forwarding class 1, high drop
	case "CS7":
		return 56 // Network Control
	case "CS6":
		return 48 // Internetwork Control
	case "CS5":
		return 40 // VOICE-ADMIT / signaling
	case "CS4":
		return 32 // Real-Time Interactive
	case "CS3":
		return 24 // Broadcast Video
	case "CS2":
		return 16 // OAM
	case "CS1":
		return 8 // Scavenger / bulk data
	case "BE", "", "default":
		return 0 // Best Effort
	default:
		return 0
	}
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
	return syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_TOS, tos)
}
