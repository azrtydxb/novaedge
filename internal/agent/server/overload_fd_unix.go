//go:build linux || darwin

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

package server

import (
	"os"
	"path/filepath"
)

// countOpenFDs returns the number of open file descriptors for the current process.
// On Linux/Darwin, it reads from /dev/fd (which is available on both platforms).
func countOpenFDs() int {
	dirPath := filepath.Clean("/dev/fd")
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return 0
	}
	// Subtract 1 for the directory fd opened by ReadDir itself (if still open),
	// but in practice the count is close enough for threshold checking.
	return len(entries)
}
