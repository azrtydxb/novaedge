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
	"flag"
	"os"
	"testing"

	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	"sigs.k8s.io/gateway-api/apis/v1beta1"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

var (
	gatewayClassName      string
	debug                 bool
	cleanup               bool
	conformanceClient     client.Client
	conformanceClientset  clientset.Interface
	conformanceRestConfig *rest.Config
)

func init() {
	flag.StringVar(&gatewayClassName, "gateway-class", "novaedge", "Name of the GatewayClass to use for conformance tests")
	flag.BoolVar(&debug, "debug", false, "Enable debug logging")
	flag.BoolVar(&cleanup, "cleanup", true, "Clean up resources after tests")
}

func TestMain(m *testing.M) {
	flag.Parse()

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		panic("failed to add clientgo scheme: " + err.Error())
	}
	if err := gatewayv1.Install(scheme); err != nil {
		panic("failed to add gateway API v1 scheme: " + err.Error())
	}
	if err := v1beta1.Install(scheme); err != nil {
		panic("failed to add gateway API v1beta1 scheme: " + err.Error())
	}

	// Load kubeconfig
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home := os.Getenv("HOME")
		if home != "" {
			kubeconfig = home + "/.kube/config"
		}
	}

	var err error
	conformanceRestConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		// Try in-cluster config
		conformanceRestConfig, err = rest.InClusterConfig()
		if err != nil {
			panic("failed to load kubeconfig: " + err.Error())
		}
	}

	conformanceClient, err = client.New(conformanceRestConfig, client.Options{Scheme: scheme})
	if err != nil {
		panic("failed to create client: " + err.Error())
	}

	conformanceClientset, err = clientset.NewForConfig(conformanceRestConfig)
	if err != nil {
		panic("failed to create clientset: " + err.Error())
	}

	os.Exit(m.Run())
}
