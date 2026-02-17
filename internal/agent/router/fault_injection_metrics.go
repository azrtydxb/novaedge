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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// FaultInjectionDelaysTotal counts the number of fault-injected delays.
	FaultInjectionDelaysTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_fault_injection_delays_total",
			Help: "Total number of fault-injected delays",
		},
		[]string{"route", "method"},
	)

	// FaultInjectionAbortsTotal counts the number of fault-injected aborts.
	FaultInjectionAbortsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_fault_injection_aborts_total",
			Help: "Total number of fault-injected aborts",
		},
		[]string{"route", "method", "status_code"},
	)
)
