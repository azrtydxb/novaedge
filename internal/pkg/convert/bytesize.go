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

package convert

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// errInvalidByteSize is returned when a byte size string cannot be parsed.
var errInvalidByteSize = errors.New("invalid byte size")

// ParseByteSize parses a human-readable byte size string (e.g., "10Mi", "1024", "50MB").
func ParseByteSize(s string) (int64, error) {
	if s == "" || s == "0" {
		return 0, nil
	}

	// Try plain integer first
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, nil
	}

	// Binary and SI units
	multipliers := map[string]int64{
		"Ki": 1 << 10,
		"Mi": 1 << 20,
		"Gi": 1 << 30,
		"Ti": 1 << 40,
		"KB": 1000,
		"MB": 1000 * 1000,
		"GB": 1000 * 1000 * 1000,
	}

	for suffix, mult := range multipliers {
		if strings.HasSuffix(s, suffix) {
			numStr := strings.TrimSuffix(s, suffix)
			n, err := strconv.ParseInt(numStr, 10, 64)
			if err != nil {
				return 0, fmt.Errorf("%w: %s", errInvalidByteSize, s)
			}
			return n * mult, nil
		}
	}

	return 0, fmt.Errorf("%w: %s", errInvalidByteSize, s)
}
