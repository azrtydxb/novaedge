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

package snapshot

import (
	"testing"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestConvertPolicyType(t *testing.T) {
	tests := []struct {
		name     string
		input    novaedgev1alpha1.PolicyType
		expected pb.PolicyType
	}{
		{
			name:     "RateLimit",
			input:    novaedgev1alpha1.PolicyTypeRateLimit,
			expected: pb.PolicyType_RATE_LIMIT,
		},
		{
			name:     "JWT",
			input:    novaedgev1alpha1.PolicyTypeJWT,
			expected: pb.PolicyType_JWT,
		},
		{
			name:     "IPAllowList",
			input:    novaedgev1alpha1.PolicyTypeIPAllowList,
			expected: pb.PolicyType_IP_ALLOW_LIST,
		},
		{
			name:     "IPDenyList",
			input:    novaedgev1alpha1.PolicyTypeIPDenyList,
			expected: pb.PolicyType_IP_DENY_LIST,
		},
		{
			name:     "CORS",
			input:    novaedgev1alpha1.PolicyTypeCORS,
			expected: pb.PolicyType_CORS,
		},
		{
			name:     "SecurityHeaders maps to SECURITY_HEADERS not UNSPECIFIED",
			input:    novaedgev1alpha1.PolicyTypeSecurityHeaders,
			expected: pb.PolicyType_SECURITY_HEADERS,
		},
		{
			name:     "unknown type maps to UNSPECIFIED",
			input:    novaedgev1alpha1.PolicyType("Unknown"),
			expected: pb.PolicyType_POLICY_TYPE_UNSPECIFIED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertPolicyType(tt.input)
			if result != tt.expected {
				t.Errorf("convertPolicyType(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
