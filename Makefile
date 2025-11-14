# NovaEdge Makefile

# Image URL to use all building/pushing image targets
IMG_CONTROLLER ?= novaedge-controller:latest
IMG_AGENT ?= novaedge-agent:latest

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build-all

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate CRD manifests.
	$(CONTROLLER_GEN) rbac:roleName=novaedge-controller-role crd webhook paths="./..." output:crd:artifacts:config=config/crd

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: generate-crds
generate-crds: manifests ## Alias for manifests target.

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter.
	$(GOLANGCI_LINT) run

.PHONY: check
check: fmt vet lint ## Run all code quality checks.

.PHONY: test
test: fmt vet ## Run tests.
	go test ./... -coverprofile cover.out

.PHONY: test-coverage
test-coverage: test ## Run tests with coverage report.
	go tool cover -html=cover.out -o coverage.html

##@ Build

.PHONY: build-controller
build-controller: fmt vet ## Build controller binary.
	go build -o bin/novaedge-controller cmd/novaedge-controller/main.go

.PHONY: build-agent
build-agent: fmt vet ## Build agent binary.
	go build -o bin/novaedge-agent cmd/novaedge-agent/main.go

.PHONY: build-all
build-all: build-controller build-agent ## Build all binaries.

.PHONY: run-controller
run-controller: fmt vet ## Run controller from your host.
	go run ./cmd/novaedge-controller/main.go

.PHONY: docker-build
docker-build: docker-build-controller docker-build-agent ## Build all docker images.

.PHONY: docker-build-controller
docker-build-controller: ## Build controller docker image.
	docker build -t ${IMG_CONTROLLER} -f Dockerfile.controller .

.PHONY: docker-build-agent
docker-build-agent: ## Build agent docker image.
	docker build -t ${IMG_AGENT} -f Dockerfile.agent .

.PHONY: docker-push
docker-push: docker-push-controller docker-push-agent ## Push all docker images.

.PHONY: docker-push-controller
docker-push-controller: ## Push controller docker image.
	docker push ${IMG_CONTROLLER}

.PHONY: docker-push-agent
docker-push-agent: ## Push agent docker image.
	docker push ${IMG_AGENT}

##@ Deployment

.PHONY: install-crds
install-crds: manifests ## Install CRDs into the K8s cluster.
	kubectl apply -f config/crd/

.PHONY: uninstall-crds
uninstall-crds: manifests ## Uninstall CRDs from the K8s cluster.
	kubectl delete -f config/crd/

.PHONY: deploy
deploy: manifests ## Deploy controller to the K8s cluster.
	kubectl apply -f config/rbac/
	kubectl apply -f config/controller/

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster.
	kubectl delete -f config/controller/ || true
	kubectl delete -f config/rbac/ || true

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint

## Tool Versions
CONTROLLER_TOOLS_VERSION ?= v0.16.5
GOLANGCI_LINT_VERSION ?= v1.62.0

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.PHONY: clean
clean: ## Clean build artifacts.
	rm -rf bin/
	rm -rf $(LOCALBIN)/
	rm -f cover.out coverage.html
