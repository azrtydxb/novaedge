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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// WAF Metrics

	// WAFRequestsBlocked tracks total requests blocked by WAF
	WAFRequestsBlocked = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_waf_requests_blocked_total",
			Help: "Total number of requests blocked by WAF",
		},
	)

	// WAFRulesMatched tracks WAF rule matches by rule ID and category
	WAFRulesMatched = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_waf_rules_matched_total",
			Help: "Total number of WAF rules matched, labeled by rule ID and category",
		},
		[]string{"rule_id", "category"},
	)

	// WAFAnomalyScore tracks WAF anomaly scores
	WAFAnomalyScore = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "novaedge_waf_anomaly_score",
			Help:    "WAF anomaly score distribution",
			Buckets: []float64{1, 2, 3, 5, 10, 15, 25, 50, 100},
		},
	)

	// WAFResponsesBlocked tracks total responses blocked by WAF response body inspection
	WAFResponsesBlocked = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "novaedge_waf_responses_blocked_total",
			Help: "Total number of responses blocked by WAF response body inspection",
		},
	)

	// WAFProcessingErrorsTotal tracks WAF processing errors with fail mode and action labels
	WAFProcessingErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "novaedge_waf_processing_errors_total",
			Help: "Total number of WAF processing errors, labeled by fail mode and action taken",
		},
		[]string{"fail_mode", "action"},
	)
)
