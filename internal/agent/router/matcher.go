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

package router

import (
	"regexp"
	"strings"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// PathMatcher matches request paths
type PathMatcher interface {
	Match(path string) bool
}

// ExactMatcher matches exact paths
type ExactMatcher struct {
	Path string
}

func (m *ExactMatcher) Match(path string) bool {
	return path == m.Path
}

// PrefixMatcher matches path prefixes
type PrefixMatcher struct {
	Prefix string
}

func (m *PrefixMatcher) Match(path string) bool {
	return strings.HasPrefix(path, m.Prefix)
}

// RegexMatcher matches paths with regex
type RegexMatcher struct {
	Pattern *regexp.Regexp
}

func (m *RegexMatcher) Match(path string) bool {
	return m.Pattern.MatchString(path)
}

// createPathMatcher creates a path matcher from a route rule
func createPathMatcher(rule *pb.RouteRule) PathMatcher {
	if len(rule.Matches) == 0 {
		return nil
	}

	// Use the first match's path (simplified for now)
	match := rule.Matches[0]
	if match.Path == nil {
		return nil
	}

	switch match.Path.Type {
	case pb.PathMatchType_EXACT:
		return &ExactMatcher{Path: match.Path.Value}
	case pb.PathMatchType_PATH_PREFIX:
		return &PrefixMatcher{Prefix: match.Path.Value}
	case pb.PathMatchType_REGULAR_EXPRESSION:
		if regex, err := regexp.Compile(match.Path.Value); err == nil {
			return &RegexMatcher{Pattern: regex}
		}
		return nil
	default:
		return nil
	}
}
