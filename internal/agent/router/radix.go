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

import "net/http"

// radixNode is a node in a compressed radix tree (Patricia trie) that indexes
// route entries by their path. Each node stores a path segment (prefix) and
// optional route entries that match at that point in the tree.
//
// The tree supports two kinds of matches:
//   - Exact: the full request path must equal the node's accumulated prefix
//   - Prefix: the request path must start with the node's accumulated prefix
//
// Routes that cannot be indexed by path (regex matchers, expression-only routes,
// catch-all routes) are stored in a separate fallback slice and checked via
// linear scan after the tree lookup.
type radixNode struct {
	prefix   string
	children []*radixNode
	// exactEntries are routes that require an exact path match at this node
	exactEntries []*RouteEntry
	// prefixEntries are routes that match any path starting with this node's accumulated prefix
	prefixEntries []*RouteEntry
}

// routeIndex is the per-hostname index built at config time. It holds a radix
// tree for path-indexable routes and a fallback list for everything else.
type routeIndex struct {
	tree     *radixNode
	fallback []*RouteEntry // regex, expression-only, and catch-all routes
}

// newRouteIndex builds a route index from a sorted slice of route entries.
// The entries should already be sorted by specificity (highest first).
func newRouteIndex(entries []*RouteEntry) *routeIndex {
	idx := &routeIndex{
		tree: &radixNode{},
	}
	for _, entry := range entries {
		switch m := entry.PathMatcher.(type) {
		case *ExactMatcher:
			idx.tree.insert(m.Path, entry, true)
		case *PrefixMatcher:
			idx.tree.insert(m.Prefix, entry, false)
		default:
			// RegexMatcher, nil (catch-all), or expression-only routes
			idx.fallback = append(idx.fallback, entry)
		}
	}
	return idx
}

// insert adds a route entry into the radix tree under the given path key.
// If exact is true, the entry is stored as an exact match; otherwise as a prefix match.
func (n *radixNode) insert(path string, entry *RouteEntry, exact bool) {
	current := n
	remaining := path

	for {
		if remaining == "" {
			if exact {
				current.exactEntries = append(current.exactEntries, entry)
			} else {
				current.prefixEntries = append(current.prefixEntries, entry)
			}
			return
		}

		// Find a child that shares a common prefix
		matched := false
		for i, child := range current.children {
			commonLen := commonPrefixLen(remaining, child.prefix)
			if commonLen == 0 {
				continue
			}

			if commonLen == len(child.prefix) {
				// The child's prefix is fully consumed; descend into it
				remaining = remaining[commonLen:]
				current = current.children[i]
				matched = true
				break
			}

			// Partial match: split the child node
			// Before: current -> child("abcdef")
			// After:  current -> newChild("abc") -> oldChild("def")
			newChild := &radixNode{
				prefix:   child.prefix[:commonLen],
				children: []*radixNode{child},
			}
			child.prefix = child.prefix[commonLen:]
			current.children[i] = newChild

			remaining = remaining[commonLen:]
			current = newChild
			matched = true
			break
		}

		if !matched {
			// No child shares a prefix; create a new leaf
			leaf := &radixNode{prefix: remaining}
			if exact {
				leaf.exactEntries = append(leaf.exactEntries, entry)
			} else {
				leaf.prefixEntries = append(leaf.prefixEntries, entry)
			}
			current.children = append(current.children, leaf)
			return
		}
	}
}

// lookup finds the best matching route entry for the given request path.
// It walks down the tree to the deepest matching node, then checks entries
// from most-specific to least-specific:
//  1. Exact entries at the deepest node (path fully consumed)
//  2. Prefix entries at the deepest node, then each ancestor back to root
//  3. Fallback entries (regex, expression-only, catch-all)
//
// This ensures longer prefixes and exact matches always win over shorter prefixes.
func (idx *routeIndex) lookup(path string, req *http.Request, matchFn func(*RouteEntry, *http.Request) bool) *RouteEntry {
	// Walk the tree to the deepest matching node, collecting ancestors with prefix entries
	current := idx.tree
	remaining := path

	// ancestors tracks nodes along the path that have prefix entries (deepest first when reversed)
	type ancestor struct {
		node *radixNode
	}
	// Pre-allocate a small stack on the stack frame (typical path depth is <8)
	ancestorBuf := [8]ancestor{}
	ancestors := ancestorBuf[:0]

	// Root may have prefix entries (e.g., "/" catch-all prefix)
	if len(current.prefixEntries) > 0 {
		ancestors = append(ancestors, ancestor{node: current})
	}

	for remaining != "" {
		found := false
		for _, child := range current.children {
			cl := commonPrefixLen(remaining, child.prefix)
			if cl == 0 {
				continue
			}
			if cl < len(child.prefix) {
				// Partial match: path diverges within this child's prefix.
				// No deeper match possible.
				break
			}

			// Full match of child prefix; descend
			remaining = remaining[cl:]
			current = child

			if len(current.prefixEntries) > 0 {
				ancestors = append(ancestors, ancestor{node: current})
			}

			found = true
			break
		}
		if !found {
			break
		}
	}

	// Phase 1: If path is fully consumed, check exact entries at the deepest node
	if remaining == "" {
		if entry := matchFirst(current.exactEntries, req, matchFn); entry != nil {
			return entry
		}
	}

	// Phase 2: Check prefix entries from deepest ancestor to shallowest (most-specific first)
	for i := len(ancestors) - 1; i >= 0; i-- {
		if entry := matchFirst(ancestors[i].node.prefixEntries, req, matchFn); entry != nil {
			return entry
		}
	}

	// Phase 3: Fall through to the fallback list (regex, expression-only, catch-all)
	return matchFirst(idx.fallback, req, matchFn)
}

// matchFirst returns the first entry in the slice that matches the request.
func matchFirst(entries []*RouteEntry, req *http.Request, matchFn func(*RouteEntry, *http.Request) bool) *RouteEntry {
	for _, entry := range entries {
		if matchFn(entry, req) {
			return entry
		}
	}
	return nil
}

// commonPrefixLen returns the length of the common prefix between two strings.
func commonPrefixLen(a, b string) int {
	maxLen := len(a)
	if len(b) < maxLen {
		maxLen = len(b)
	}
	for i := 0; i < maxLen; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return maxLen
}
