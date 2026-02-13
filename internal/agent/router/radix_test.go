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
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

const nilName = "<nil>"

// alwaysMatch is a match function that always returns true (for testing tree structure)
func alwaysMatch(entry *RouteEntry, _ *http.Request) bool {
	return entry != nil
}

func TestCommonPrefixLen(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 0},
		{"", "abc", 0},
		{"abc", "abc", 3},
		{"abc", "abd", 2},
		{"abc", "xyz", 0},
		{"/api/v1", "/api/v2", 6},
		{"/api/v1/users", "/api/v1/posts", 8},
	}
	for _, tt := range tests {
		got := commonPrefixLen(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("commonPrefixLen(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestRadixTreeExactMatch(t *testing.T) {
	entry1 := &RouteEntry{
		Route:       &pb.Route{Name: "exact-users"},
		Rule:        &pb.RouteRule{},
		PathMatcher: &ExactMatcher{Path: "/api/users"},
	}
	entry2 := &RouteEntry{
		Route:       &pb.Route{Name: "exact-posts"},
		Rule:        &pb.RouteRule{},
		PathMatcher: &ExactMatcher{Path: "/api/posts"},
	}

	idx := newRouteIndex([]*RouteEntry{entry1, entry2})

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	got := idx.lookup("/api/users", req, alwaysMatch)
	if got != entry1 {
		t.Errorf("expected entry1 for /api/users, got %v", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/posts", nil)
	got = idx.lookup("/api/posts", req, alwaysMatch)
	if got != entry2 {
		t.Errorf("expected entry2 for /api/posts, got %v", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/other", nil)
	got = idx.lookup("/api/other", req, alwaysMatch)
	if got != nil {
		t.Errorf("expected nil for /api/other, got %v", got)
	}
}

func TestRadixTreePrefixMatch(t *testing.T) {
	entry1 := &RouteEntry{
		Route:       &pb.Route{Name: "prefix-api"},
		Rule:        &pb.RouteRule{},
		PathMatcher: &PrefixMatcher{Prefix: "/api/"},
	}
	entry2 := &RouteEntry{
		Route:       &pb.Route{Name: "prefix-static"},
		Rule:        &pb.RouteRule{},
		PathMatcher: &PrefixMatcher{Prefix: "/static/"},
	}

	idx := newRouteIndex([]*RouteEntry{entry1, entry2})

	tests := []struct {
		path string
		want *RouteEntry
	}{
		{"/api/users", entry1},
		{"/api/posts/123", entry1},
		{"/static/css/app.css", entry2},
		{"/other", nil},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		got := idx.lookup(tt.path, req, alwaysMatch)
		if got != tt.want {
			name := nilName
			if got != nil {
				name = got.Route.Name
			}
			wantName := nilName
			if tt.want != nil {
				wantName = tt.want.Route.Name
			}
			t.Errorf("lookup(%q) = %s, want %s", tt.path, name, wantName)
		}
	}
}

func TestRadixTreeExactBeatsPrefix(t *testing.T) {
	// When both exact and prefix match, exact should win (inserted first due to specificity sort)
	exactEntry := &RouteEntry{
		Route:       &pb.Route{Name: "exact"},
		Rule:        &pb.RouteRule{},
		PathMatcher: &ExactMatcher{Path: "/api/users"},
	}
	prefixEntry := &RouteEntry{
		Route:       &pb.Route{Name: "prefix"},
		Rule:        &pb.RouteRule{},
		PathMatcher: &PrefixMatcher{Prefix: "/api/"},
	}

	// Sorted by specificity: exact first, then prefix
	idx := newRouteIndex([]*RouteEntry{exactEntry, prefixEntry})

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	got := idx.lookup("/api/users", req, alwaysMatch)
	if got != exactEntry {
		t.Errorf("expected exact entry, got %v", got)
	}

	// Sub-paths should still match the prefix entry
	req = httptest.NewRequest(http.MethodGet, "/api/users/123", nil)
	got = idx.lookup("/api/users/123", req, alwaysMatch)
	if got != prefixEntry {
		t.Errorf("expected prefix entry for sub-path, got %v", got)
	}
}

func TestRadixTreeRegexFallback(t *testing.T) {
	regexEntry := &RouteEntry{
		Route:       &pb.Route{Name: "regex"},
		Rule:        &pb.RouteRule{},
		PathMatcher: &RegexMatcher{Pattern: regexp.MustCompile(`^/api/v\d+/.*`)},
	}
	exactEntry := &RouteEntry{
		Route:       &pb.Route{Name: "exact"},
		Rule:        &pb.RouteRule{},
		PathMatcher: &ExactMatcher{Path: "/health"},
	}

	idx := newRouteIndex([]*RouteEntry{exactEntry, regexEntry})

	// Regex entries go to fallback
	if len(idx.fallback) != 1 {
		t.Fatalf("expected 1 fallback entry, got %d", len(idx.fallback))
	}
	if idx.fallback[0] != regexEntry {
		t.Error("expected regex entry in fallback")
	}

	// Use a match function that respects the PathMatcher
	matchFn := func(entry *RouteEntry, req *http.Request) bool {
		if entry.PathMatcher == nil {
			return true
		}
		return entry.PathMatcher.Match(req.URL.Path)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	got := idx.lookup("/api/v1/users", req, matchFn)
	if got != regexEntry {
		t.Error("expected regex entry for /api/v1/users")
	}

	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	got = idx.lookup("/health", req, matchFn)
	if got != exactEntry {
		t.Error("expected exact entry for /health")
	}
}

func TestRadixTreeCatchAll(t *testing.T) {
	catchAll := &RouteEntry{
		Route:       &pb.Route{Name: "catch-all"},
		Rule:        &pb.RouteRule{},
		PathMatcher: nil, // nil matcher = catch-all
	}
	exactEntry := &RouteEntry{
		Route:       &pb.Route{Name: "exact"},
		Rule:        &pb.RouteRule{},
		PathMatcher: &ExactMatcher{Path: "/known"},
	}

	// Sorted: exact first, then catch-all
	idx := newRouteIndex([]*RouteEntry{exactEntry, catchAll})

	if len(idx.fallback) != 1 || idx.fallback[0] != catchAll {
		t.Fatal("expected catch-all in fallback")
	}

	req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	got := idx.lookup("/unknown", req, alwaysMatch)
	if got != catchAll {
		t.Error("expected catch-all for /unknown")
	}
}

func TestRadixTreeNestedPrefixes(t *testing.T) {
	// Test that longer prefixes are preferred over shorter ones
	shortPrefix := &RouteEntry{
		Route:       &pb.Route{Name: "short"},
		Rule:        &pb.RouteRule{},
		PathMatcher: &PrefixMatcher{Prefix: "/api/"},
	}
	longPrefix := &RouteEntry{
		Route:       &pb.Route{Name: "long"},
		Rule:        &pb.RouteRule{},
		PathMatcher: &PrefixMatcher{Prefix: "/api/v1/"},
	}

	// Sorted by specificity: longer prefix first
	idx := newRouteIndex([]*RouteEntry{longPrefix, shortPrefix})

	matchFn := func(entry *RouteEntry, req *http.Request) bool {
		if entry.PathMatcher == nil {
			return true
		}
		return entry.PathMatcher.Match(req.URL.Path)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	got := idx.lookup("/api/v1/users", req, matchFn)
	if got != longPrefix {
		name := nilName
		if got != nil {
			name = got.Route.Name
		}
		t.Errorf("expected long prefix entry for /api/v1/users, got %s", name)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v2/users", nil)
	got = idx.lookup("/api/v2/users", req, matchFn)
	if got != shortPrefix {
		name := nilName
		if got != nil {
			name = got.Route.Name
		}
		t.Errorf("expected short prefix entry for /api/v2/users, got %s", name)
	}
}

func BenchmarkRadixLookup(b *testing.B) {
	entries := make([]*RouteEntry, 0, 100)
	paths := []string{
		"/api/v1/users", "/api/v1/posts", "/api/v1/comments",
		"/api/v2/users", "/api/v2/posts", "/api/v2/comments",
		"/static/css", "/static/js", "/static/images",
		"/health", "/ready", "/metrics",
	}
	for _, p := range paths {
		entries = append(entries, &RouteEntry{
			Route:       &pb.Route{Name: p},
			Rule:        &pb.RouteRule{},
			PathMatcher: &ExactMatcher{Path: p},
		})
	}
	// Add some prefix routes
	for _, p := range []string{"/api/v1/", "/api/v2/", "/static/"} {
		entries = append(entries, &RouteEntry{
			Route:       &pb.Route{Name: "prefix-" + p},
			Rule:        &pb.RouteRule{},
			PathMatcher: &PrefixMatcher{Prefix: p},
		})
	}

	idx := newRouteIndex(entries)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		idx.lookup("/api/v1/users", req, alwaysMatch)
	}
}

func BenchmarkLinearScanLookup(b *testing.B) {
	entries := make([]*RouteEntry, 0, 100)
	paths := []string{
		"/api/v1/users", "/api/v1/posts", "/api/v1/comments",
		"/api/v2/users", "/api/v2/posts", "/api/v2/comments",
		"/static/css", "/static/js", "/static/images",
		"/health", "/ready", "/metrics",
	}
	for _, p := range paths {
		entries = append(entries, &RouteEntry{
			Route:       &pb.Route{Name: p},
			Rule:        &pb.RouteRule{},
			PathMatcher: &ExactMatcher{Path: p},
		})
	}
	for _, p := range []string{"/api/v1/", "/api/v2/", "/static/"} {
		entries = append(entries, &RouteEntry{
			Route:       &pb.Route{Name: "prefix-" + p},
			Rule:        &pb.RouteRule{},
			PathMatcher: &PrefixMatcher{Prefix: p},
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	matchFn := func(entry *RouteEntry, r *http.Request) bool {
		if entry.PathMatcher == nil {
			return true
		}
		return entry.PathMatcher.Match(r.URL.Path)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, entry := range entries {
			if matchFn(entry, req) {
				break
			}
		}
	}
}
