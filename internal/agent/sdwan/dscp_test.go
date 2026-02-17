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

package sdwan

import "testing"

func TestDSCPClassToValue(t *testing.T) {
	tests := []struct {
		class string
		want  int
	}{
		{"EF", 46},
		{"AF41", 34},
		{"AF42", 36},
		{"AF43", 38},
		{"AF31", 26},
		{"AF32", 28},
		{"AF33", 30},
		{"AF21", 18},
		{"AF22", 20},
		{"AF23", 22},
		{"AF11", 10},
		{"AF12", 12},
		{"AF13", 14},
		{"CS7", 56},
		{"CS6", 48},
		{"CS5", 40},
		{"CS4", 32},
		{"CS3", 24},
		{"CS2", 16},
		{"CS1", 8},
		{"BE", 0},
		{"default", 0},
		{"", 0},
		{"unknown", 0},
		{"INVALID", 0},
	}

	for _, tt := range tests {
		t.Run(tt.class, func(t *testing.T) {
			got := DSCPClassToValue(tt.class)
			if got != tt.want {
				t.Errorf("DSCPClassToValue(%q) = %d, want %d", tt.class, got, tt.want)
			}
		})
	}
}

func TestDSCPToTOS(t *testing.T) {
	tests := []struct {
		dscp int
		want int
	}{
		{0, 0},
		{8, 32},   // CS1: 8 << 2 = 32
		{10, 40},  // AF11: 10 << 2 = 40
		{18, 72},  // AF21: 18 << 2 = 72
		{26, 104}, // AF31: 26 << 2 = 104
		{34, 136}, // AF41: 34 << 2 = 136
		{46, 184}, // EF: 46 << 2 = 184
		{48, 192}, // CS6: 48 << 2 = 192
		{56, 224}, // CS7: 56 << 2 = 224
	}

	for _, tt := range tests {
		got := DSCPToTOS(tt.dscp)
		if got != tt.want {
			t.Errorf("DSCPToTOS(%d) = %d, want %d", tt.dscp, got, tt.want)
		}
	}
}

func TestDSCPToTOS_RoundTrip(t *testing.T) {
	// For each DSCP class, converting to TOS and back should yield the same DSCP.
	classes := []string{"EF", "AF41", "AF31", "AF21", "AF11", "CS1", "CS6"}
	for _, class := range classes {
		dscp := DSCPClassToValue(class)
		tos := DSCPToTOS(dscp)
		recovered := tos >> 2
		if recovered != dscp {
			t.Errorf("round-trip failed for %s: dscp=%d, tos=%d, recovered=%d", class, dscp, tos, recovered)
		}
	}
}

func TestSetDSCPOnSocket_BestEffortIsNoop(t *testing.T) {
	// SetDSCPOnSocket with BE should return nil without calling setsockopt.
	// We pass an invalid fd (0) to verify the syscall is not attempted.
	err := SetDSCPOnSocket(0, "BE")
	if err != nil {
		t.Errorf("SetDSCPOnSocket with BE should be no-op, got error: %v", err)
	}

	err = SetDSCPOnSocket(0, "")
	if err != nil {
		t.Errorf("SetDSCPOnSocket with empty class should be no-op, got error: %v", err)
	}

	err = SetDSCPOnSocket(0, "default")
	if err != nil {
		t.Errorf("SetDSCPOnSocket with default class should be no-op, got error: %v", err)
	}
}
