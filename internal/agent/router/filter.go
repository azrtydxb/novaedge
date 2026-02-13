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

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

// Filter represents a request/response filter
type Filter interface {
	Apply(w http.ResponseWriter, r *http.Request) (*http.Request, bool)
}

// HeaderModifierFilter modifies request headers
type HeaderModifierFilter struct {
	filter *pb.RouteFilter
}

// NewHeaderModifierFilter creates a new header modifier filter
func NewHeaderModifierFilter(filter *pb.RouteFilter) *HeaderModifierFilter {
	return &HeaderModifierFilter{filter: filter}
}

// Apply modifies request headers
func (f *HeaderModifierFilter) Apply(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	// Add headers
	for _, header := range f.filter.AddHeaders {
		r.Header.Add(header.Name, header.Value)
	}

	// Remove headers
	for _, name := range f.filter.RemoveHeaders {
		r.Header.Del(name)
	}

	return r, true
}

// RedirectFilter handles HTTP redirects
type RedirectFilter struct {
	filter *pb.RouteFilter
}

// NewRedirectFilter creates a new redirect filter
func NewRedirectFilter(filter *pb.RouteFilter) *RedirectFilter {
	return &RedirectFilter{filter: filter}
}

// Apply performs HTTP redirect
func (f *RedirectFilter) Apply(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	if f.filter.RedirectUrl == "" {
		return r, true
	}

	// Send redirect response (302 default)
	http.Redirect(w, r, f.filter.RedirectUrl, http.StatusFound)

	// Return false to stop further processing
	return r, false
}

// URLRewriteFilter rewrites request URLs
type URLRewriteFilter struct {
	filter *pb.RouteFilter
}

// NewURLRewriteFilter creates a new URL rewrite filter
func NewURLRewriteFilter(filter *pb.RouteFilter) *URLRewriteFilter {
	return &URLRewriteFilter{filter: filter}
}

// Apply rewrites request URL
func (f *URLRewriteFilter) Apply(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	if f.filter.RewritePath == "" {
		return r, true
	}

	// Rewrite path
	r.URL.Path = f.filter.RewritePath
	r.RequestURI = f.filter.RewritePath
	if r.URL.RawQuery != "" {
		r.RequestURI += "?" + r.URL.RawQuery
	}

	return r, true
}

// buildFilters pre-builds filter instances at config time to avoid per-request allocations.
// Filters are created once during ApplyConfig and stored on the RouteEntry.
func buildFilters(pbFilters []*pb.RouteFilter) []Filter {
	filters := make([]Filter, 0, len(pbFilters))
	for _, pbFilter := range pbFilters {
		switch pbFilter.Type {
		case pb.RouteFilterType_ADD_HEADER, pb.RouteFilterType_REMOVE_HEADER:
			filters = append(filters, NewHeaderModifierFilter(pbFilter))
		case pb.RouteFilterType_REQUEST_REDIRECT:
			filters = append(filters, NewRedirectFilter(pbFilter))
		case pb.RouteFilterType_URL_REWRITE:
			filters = append(filters, NewURLRewriteFilter(pbFilter))
		default:
			// Unknown filter type, skip
			continue
		}
	}
	return filters
}

// applyPrebuiltFilters applies pre-built filters to a request, avoiding per-request
// filter instantiation.
func applyPrebuiltFilters(filters []Filter, w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	for _, filter := range filters {
		newReq, shouldContinue := filter.Apply(w, r)
		r = newReq
		if !shouldContinue {
			return r, false
		}
	}
	return r, true
}
