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

package proto

// SessionAffinityConfig defines session affinity (sticky sessions) configuration.
// This message extends the proto Cluster with session persistence settings.
type SessionAffinityConfig struct {
	// Type specifies the affinity mechanism: "cookie", "header", or "source_ip"
	Type string `protobuf:"bytes,1,opt,name=type,proto3" json:"type,omitempty"`

	// CookieName is the name of the cookie for cookie-based affinity
	CookieName string `protobuf:"bytes,2,opt,name=cookie_name,json=cookieName,proto3" json:"cookie_name,omitempty"`

	// CookieTtlSeconds is the TTL for the affinity cookie in seconds (0 = session cookie)
	CookieTtlSeconds int64 `protobuf:"varint,3,opt,name=cookie_ttl_seconds,json=cookieTtlSeconds,proto3" json:"cookie_ttl_seconds,omitempty"`

	// CookiePath is the path attribute for the affinity cookie
	CookiePath string `protobuf:"bytes,4,opt,name=cookie_path,json=cookiePath,proto3" json:"cookie_path,omitempty"`

	// CookieSecure sets the Secure flag on the affinity cookie
	CookieSecure bool `protobuf:"varint,5,opt,name=cookie_secure,json=cookieSecure,proto3" json:"cookie_secure,omitempty"`

	// CookieSameSite sets the SameSite attribute: "Strict", "Lax", or "None"
	CookieSameSite string `protobuf:"bytes,6,opt,name=cookie_same_site,json=cookieSameSite,proto3" json:"cookie_same_site,omitempty"`
}

// GetType returns the affinity type.
func (x *SessionAffinityConfig) GetType() string {
	if x != nil {
		return x.Type
	}
	return ""
}

// GetCookieName returns the cookie name.
func (x *SessionAffinityConfig) GetCookieName() string {
	if x != nil {
		return x.CookieName
	}
	return ""
}

// GetCookieTtlSeconds returns the cookie TTL in seconds.
func (x *SessionAffinityConfig) GetCookieTtlSeconds() int64 {
	if x != nil {
		return x.CookieTtlSeconds
	}
	return 0
}

// GetCookiePath returns the cookie path.
func (x *SessionAffinityConfig) GetCookiePath() string {
	if x != nil {
		return x.CookiePath
	}
	return ""
}

// GetCookieSecure returns the cookie Secure flag.
func (x *SessionAffinityConfig) GetCookieSecure() bool {
	if x != nil {
		return x.CookieSecure
	}
	return false
}

// GetCookieSameSite returns the cookie SameSite attribute.
func (x *SessionAffinityConfig) GetCookieSameSite() string {
	if x != nil {
		return x.CookieSameSite
	}
	return ""
}
