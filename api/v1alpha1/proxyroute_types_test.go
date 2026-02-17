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

package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPathMatchTypeConstants(t *testing.T) {
	assert.Equal(t, PathMatchType("Exact"), PathMatchExact)
	assert.Equal(t, PathMatchType("PathPrefix"), PathMatchPathPrefix)
	assert.Equal(t, PathMatchType("RegularExpression"), PathMatchRegularExpression)
}

func TestHeaderMatchTypeConstants(t *testing.T) {
	assert.Equal(t, HeaderMatchType("Exact"), HeaderMatchExact)
	assert.Equal(t, HeaderMatchType("RegularExpression"), HeaderMatchRegularExpression)
}

func TestHTTPPathMatch(t *testing.T) {
	tests := []struct {
		name      string
		pathMatch HTTPPathMatch
	}{
		{
			name: "exact match",
			pathMatch: HTTPPathMatch{
				Type:  PathMatchExact,
				Value: "/api/v1",
			},
		},
		{
			name: "prefix match",
			pathMatch: HTTPPathMatch{
				Type:  PathMatchPathPrefix,
				Value: "/api",
			},
		},
		{
			name: "regex match",
			pathMatch: HTTPPathMatch{
				Type:  PathMatchRegularExpression,
				Value: "^/api/v[0-9]+",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotEmpty(t, tt.pathMatch.Type)
			assert.NotEmpty(t, tt.pathMatch.Value)
		})
	}
}

func TestHTTPHeaderMatch(t *testing.T) {
	match := HTTPHeaderMatch{
		Type:  HeaderMatchExact,
		Name:  "Content-Type",
		Value: "application/json",
	}

	assert.Equal(t, HeaderMatchExact, match.Type)
	assert.Equal(t, "Content-Type", match.Name)
	assert.Equal(t, "application/json", match.Value)
}

func TestHTTPQueryParamMatch(t *testing.T) {
	match := HTTPQueryParamMatch{
		Type:  HeaderMatchExact,
		Name:  "version",
		Value: "v1",
	}

	assert.Equal(t, HeaderMatchExact, match.Type)
	assert.Equal(t, "version", match.Name)
	assert.Equal(t, "v1", match.Value)
}

func TestHTTPRouteMatch(t *testing.T) {
	path := HTTPPathMatch{
		Type:  PathMatchPathPrefix,
		Value: "/api",
	}

	routeMatch := HTTPRouteMatch{
		Path: &path,
		Headers: []HTTPHeaderMatch{
			{Name: "X-Custom-Header", Value: "test"},
		},
		QueryParams: []HTTPQueryParamMatch{
			{Name: "debug", Value: "true"},
		},
	}

	assert.NotNil(t, routeMatch.Path)
	assert.Equal(t, PathMatchPathPrefix, routeMatch.Path.Type)
	assert.Len(t, routeMatch.Headers, 1)
	assert.Len(t, routeMatch.QueryParams, 1)
}

func TestHTTPRouteMatch_NilFields(t *testing.T) {
	routeMatch := HTTPRouteMatch{}

	assert.Nil(t, routeMatch.Path)
	assert.Nil(t, routeMatch.Headers)
	assert.Nil(t, routeMatch.QueryParams)
}
