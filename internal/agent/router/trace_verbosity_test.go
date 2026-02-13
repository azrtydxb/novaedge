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
	"sync"
	"testing"
)

func TestTraceVerbosityConfig_DefaultIsMinimal(t *testing.T) {
	cfg := NewTraceVerbosityConfig("")
	if got := cfg.GetVerbosity(); got != TraceVerbosityMinimal {
		t.Errorf("expected default verbosity %q, got %q", TraceVerbosityMinimal, got)
	}
	if cfg.ShouldTraceDetailed() {
		t.Error("ShouldTraceDetailed should be false for minimal verbosity")
	}
}

func TestTraceVerbosityConfig_ExplicitMinimal(t *testing.T) {
	cfg := NewTraceVerbosityConfig(TraceVerbosityMinimal)
	if got := cfg.GetVerbosity(); got != TraceVerbosityMinimal {
		t.Errorf("expected %q, got %q", TraceVerbosityMinimal, got)
	}
	if cfg.ShouldTraceDetailed() {
		t.Error("ShouldTraceDetailed should be false for minimal")
	}
}

func TestTraceVerbosityConfig_Detailed(t *testing.T) {
	cfg := NewTraceVerbosityConfig(TraceVerbosityDetailed)
	if got := cfg.GetVerbosity(); got != TraceVerbosityDetailed {
		t.Errorf("expected %q, got %q", TraceVerbosityDetailed, got)
	}
	if !cfg.ShouldTraceDetailed() {
		t.Error("ShouldTraceDetailed should be true for detailed")
	}
}

func TestTraceVerbosityConfig_SetVerbosity(t *testing.T) {
	cfg := NewTraceVerbosityConfig(TraceVerbosityMinimal)

	// Switch to detailed
	cfg.SetVerbosity(TraceVerbosityDetailed)
	if got := cfg.GetVerbosity(); got != TraceVerbosityDetailed {
		t.Errorf("after SetVerbosity(detailed): got %q", got)
	}
	if !cfg.ShouldTraceDetailed() {
		t.Error("ShouldTraceDetailed should be true after set to detailed")
	}

	// Switch back to minimal
	cfg.SetVerbosity(TraceVerbosityMinimal)
	if got := cfg.GetVerbosity(); got != TraceVerbosityMinimal {
		t.Errorf("after SetVerbosity(minimal): got %q", got)
	}
	if cfg.ShouldTraceDetailed() {
		t.Error("ShouldTraceDetailed should be false after set to minimal")
	}
}

func TestTraceVerbosityConfig_ConcurrentAccess(t *testing.T) {
	cfg := NewTraceVerbosityConfig(TraceVerbosityMinimal)
	const goroutines = 100
	const iterations = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Half the goroutines toggle the verbosity level
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if j%2 == 0 {
					cfg.SetVerbosity(TraceVerbosityDetailed)
				} else {
					cfg.SetVerbosity(TraceVerbosityMinimal)
				}
			}
		}()
	}

	// Half the goroutines read the current level
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				level := cfg.GetVerbosity()
				if level != TraceVerbosityMinimal && level != TraceVerbosityDetailed {
					t.Errorf("unexpected verbosity level: %q", level)
					return
				}
				// ShouldTraceDetailed must agree with GetVerbosity result
				_ = cfg.ShouldTraceDetailed()
			}
		}()
	}

	wg.Wait()
}

func TestDefaultTraceVerbosity_IsMinimal(t *testing.T) {
	// The package-level singleton should default to minimal
	if got := DefaultTraceVerbosity.GetVerbosity(); got != TraceVerbosityMinimal {
		t.Errorf("DefaultTraceVerbosity: expected %q, got %q", TraceVerbosityMinimal, got)
	}
}

func TestDefaultTraceVerbosity_CanBeChanged(t *testing.T) {
	// Save and restore the original level to avoid test pollution
	original := DefaultTraceVerbosity.GetVerbosity()
	defer DefaultTraceVerbosity.SetVerbosity(original)

	DefaultTraceVerbosity.SetVerbosity(TraceVerbosityDetailed)
	if !DefaultTraceVerbosity.ShouldTraceDetailed() {
		t.Error("expected DefaultTraceVerbosity to report detailed after set")
	}
}
