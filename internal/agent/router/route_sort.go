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

import "sort"

// sortRoutesBySpecificity sorts routes so the most specific matches are tried first.
// Order: exact path > longest prefix > regex > catch-all (no matches).
// Within each category, routes with more match conditions (headers, method)
// are sorted before routes with fewer conditions.
// This allows ServeHTTP to break early on the first match.
func sortRoutesBySpecificity(routes []*RouteEntry) {
	sort.SliceStable(routes, func(i, j int) bool {
		return routeSpecificity(routes[i]) > routeSpecificity(routes[j])
	})
}

// routeSpecificity returns a numeric score for a route entry.
// Higher values indicate more specific routes that should be matched first.
func routeSpecificity(entry *RouteEntry) int {
	score := 0

	// Boolean expression adds specificity
	if entry.Expression != nil {
		score += 100
	}

	if len(entry.Rule.Matches) == 0 {
		// Catch-all: lowest specificity
		return score
	}

	// Use the most specific match among all match conditions
	bestMatchScore := 0
	for _, match := range entry.Rule.Matches {
		matchScore := 0

		// Path specificity
		if match.Path != nil {
			switch entry.PathMatcher.(type) {
			case *ExactMatcher:
				matchScore += 3000
			case *PrefixMatcher:
				// Longer prefixes are more specific
				matchScore += 2000 + len(match.Path.Value)
			case *RegexMatcher:
				matchScore += 1000
			}
		}

		// Method match adds specificity
		if match.Method != "" {
			matchScore += 500
		}

		// Header matches add specificity
		matchScore += len(match.Headers) * 250

		if matchScore > bestMatchScore {
			bestMatchScore = matchScore
		}
	}

	return score + bestMatchScore
}
