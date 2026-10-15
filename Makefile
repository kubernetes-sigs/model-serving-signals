# Copyright The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

IMAGE ?= model-serving-signals/exporter:dev
KIND_CLUSTER_NAME ?= kind

LOCALBIN ?= $(shell pwd)/bin
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint
GOLANGCI_LINT_VERSION ?= v2.15.0

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

$(LOCALBIN):
	mkdir -p $(LOCALBIN)

.PHONY: golangci-lint
golangci-lint: $(LOCALBIN) ## Install golangci-lint locally if necessary.
	@test -x $(GOLANGCI_LINT) && $(GOLANGCI_LINT) version | grep -q $(patsubst v%,%,$(GOLANGCI_LINT_VERSION)) || \
		GOBIN=$(LOCALBIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.PHONY: lint
lint: golangci-lint ## Run golangci-lint against code.
	$(GOLANGCI_LINT) run

.PHONY: verify-boilerplate
verify-boilerplate: ## Verify license boilerplate headers on source files.
	./hack/verify-boilerplate.sh

.PHONY: test
test: fmt vet lint verify-boilerplate ## Run tests.
	go test ./...

.PHONY: image-build
image-build: ## Build the docker image.
	docker build -t $(IMAGE) .

.PHONY: image-load
image-load: image-build ## Build the image and load it in the local kind cluster.
	kind load docker-image $(IMAGE) --name $(KIND_CLUSTER_NAME)
