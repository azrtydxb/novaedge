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

package cmd

import "errors"

// Shared sentinel errors for CLI commands.
var (
	errExactlyOneArgumentRequiredNodeName             = errors.New("exactly one argument required: node-name")
	errExactlyTwoArgumentsRequiredResourceTypeAndName = errors.New("exactly two arguments required: resource-type and name")
	errNoAgentFoundOnNode                             = errors.New("no agent found on node")
	errUnknownResourceType                            = errors.New("unknown resource type")
)
