//go:build conformance

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

package conformance

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/gateway-api/conformance"
	confapis "sigs.k8s.io/gateway-api/conformance/apis/v1"
	"sigs.k8s.io/gateway-api/conformance/tests"
	"sigs.k8s.io/gateway-api/conformance/utils/suite"
)

// TestConformance runs the Gateway API conformance test suite against NovaEdge.
// This requires a running Kubernetes cluster with NovaEdge deployed.
//
// Run with: go test -v -tags conformance ./test/conformance/ -args -gateway-class=novaedge
func TestConformance(t *testing.T) {
	opts := suite.ConformanceOptions{
		Client:               conformanceClient,
		Clientset:            conformanceClientset,
		RestConfig:           conformanceRestConfig,
		GatewayClassName:     gatewayClassName,
		Debug:                debug,
		CleanupBaseResources: cleanup,
		BaseManifests:        conformance.GatewayClassBaseManifests,
		SupportedFeatures:    AllNovaEdgeSupportedFeatures(),
		Implementation: confapis.Implementation{
			Organization: "novaedge",
			Project:      "novaedge",
			URL:          "https://github.com/azrtydxb/novaedge",
			Version:      "v0.1.0",
			Contact:      []string{"@azrtydxb"},
		},
		ConformanceProfiles: sets.New(
			suite.GatewayHTTPConformanceProfileName,
			suite.GatewayGRPCConformanceProfileName,
			suite.GatewayTLSConformanceProfileName,
		),
	}

	cSuite, err := suite.NewConformanceTestSuite(opts)
	if err != nil {
		t.Fatalf("Failed to create conformance test suite: %v", err)
	}

	cSuite.Setup(t, tests.ConformanceTests)

	if err := cSuite.Run(t, tests.ConformanceTests); err != nil {
		t.Fatalf("Failed to run conformance tests: %v", err)
	}

	report, err := cSuite.Report()
	if err != nil {
		t.Fatalf("Failed to generate conformance report: %v", err)
	}

	t.Logf("Conformance report: %d profile(s) tested", len(report.ProfileReports))
	for _, profileReport := range report.ProfileReports {
		t.Logf("  Profile: %s, Core: %s, Extended: %s",
			profileReport.Name,
			profileReport.Core.Result,
			profileReport.Extended.Result,
		)
	}
}
