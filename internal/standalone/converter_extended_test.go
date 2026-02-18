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

	"github.com/stretchr/testify/assert"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

func TestConverter_parseHeaderMatchType(t *testing.T) {
	converter := NewConverter()

	tests := []struct {
		name     string
		input    string
		expected pb.HeaderMatchType
	}{
		{
			name:     "exact match",
			input:    "Exact",
			expected: pb.HeaderMatchType_HEADER_EXACT,
		},
		{
			name:     "empty string defaults to exact",
			input:    "",
			expected: pb.HeaderMatchType_HEADER_EXACT,
		},
		{
			name:     "regular expression",
			input:    "RegularExpression",
			expected: pb.HeaderMatchType_HEADER_REGULAR_EXPRESSION,
		},
		{
			name:     "unknown defaults to exact",
			input:    "Unknown",
			expected: pb.HeaderMatchType_HEADER_EXACT,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := converter.parseHeaderMatchType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConverter_parseFilterType(t *testing.T) {
	converter := NewConverter()

	tests := []struct {
		name     string
		input    string
		expected pb.RouteFilterType
	}{
		{
			name:     "add header",
			input:    "AddHeader",
			expected: pb.RouteFilterType_ADD_HEADER,
		},
		{
			name:     "remove header",
			input:    "RemoveHeader",
			expected: pb.RouteFilterType_REMOVE_HEADER,
		},
		{
			name:     "url rewrite",
			input:    "URLRewrite",
			expected: pb.RouteFilterType_URL_REWRITE,
		},
		{
			name:     "request redirect",
			input:    "RequestRedirect",
			expected: pb.RouteFilterType_REQUEST_REDIRECT,
		},
		{
			name:     "unknown defaults to unspecified",
			input:    "Unknown",
			expected: pb.RouteFilterType_ROUTE_FILTER_TYPE_UNSPECIFIED,
		},
		{
			name:     "empty defaults to unspecified",
			input:    "",
			expected: pb.RouteFilterType_ROUTE_FILTER_TYPE_UNSPECIFIED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := converter.parseFilterType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConverter_parsePathMatchType(t *testing.T) {
	converter := NewConverter()

	tests := []struct {
		name     string
		input    string
		expected pb.PathMatchType
	}{
		{
			name:     "exact",
			input:    "Exact",
			expected: pb.PathMatchType_EXACT,
		},
		{
			name:     "path prefix",
			input:    "PathPrefix",
			expected: pb.PathMatchType_PATH_PREFIX,
		},
		{
			name:     "regular expression",
			input:    "RegularExpression",
			expected: pb.PathMatchType_REGULAR_EXPRESSION,
		},
		{
			name:     "empty defaults to prefix",
			input:    "",
			expected: pb.PathMatchType_PATH_PREFIX,
		},
		{
			name:     "unknown defaults to prefix",
			input:    "Unknown",
			expected: pb.PathMatchType_PATH_PREFIX,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := converter.parsePathMatchType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
