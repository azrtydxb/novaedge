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

package vip

const (
	// msgVIPAlreadyActive is logged when a VIP is already active
	msgVIPAlreadyActive = "VIP already active"

	// msgVIPNotActive is logged when a VIP removal is requested but it's not active
	msgVIPNotActive = "VIP not active"

	// errInvalidVIPAddressFmt is the format string for invalid VIP address errors
	errInvalidVIPAddressFmt = "invalid VIP address %s: %w"

	// neighborStateDown represents OSPF neighbor Down state
	neighborStateDown = "Down"
)
