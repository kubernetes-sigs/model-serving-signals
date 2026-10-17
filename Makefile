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

# GIT_TAG is a git-based tag of the form vYYYYMMDD-hash. In postsubmit builds it
# is supplied by the cloud build via the _GIT_TAG substitution.
GIT_TAG ?= $(shell git describe --tags --dirty --always)

DOCKER_BUILDX_CMD ?= docker buildx
IMAGE_BUILD_CMD ?= $(DOCKER_BUILDX_CMD) build
PLATFORMS ?= linux/amd64,linux/arm64
IMAGE_BUILD_EXTRA_OPTS ?=

# STAGING_IMAGE_REGISTRY is the Kubernetes staging Artifact Registry. The image
# promoter copies images from here to registry.k8s.io on release.
STAGING_IMAGE_REGISTRY ?= us-central1-docker.pkg.dev/k8s-staging-images
IMAGE_REGISTRY ?= $(STAGING_IMAGE_REGISTRY)/model-serving-signals
IMAGE_NAME ?= metrics-exporter
IMAGE_REPO ?= $(IMAGE_REGISTRY)/$(IMAGE_NAME)
IMAGE_TAG ?= $(IMAGE_REPO):$(GIT_TAG)

# IMAGE is used by the local dev / kind targets.
IMAGE ?= model-serving-signals/exporter:dev
KIND_CLUSTER_NAME ?= kind

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

.PHONY: test
test: fmt vet ## Run tests.
	go test ./...

.PHONY: image-build
image-build: ## Build the docker image.
	docker build -t $(IMAGE) .

.PHONY: image-load
image-load: image-build ## Build the image and load it in the local kind cluster.
	kind load docker-image $(IMAGE) --name $(KIND_CLUSTER_NAME)

##@ Release

# EXTRA_TAG, when set (e.g. to _PULL_BASE_REF in postsubmit builds), adds a
# second tag such as :main alongside the git tag.
ifdef EXTRA_TAG
IMAGE_BUILD_EXTRA_OPTS += -t $(IMAGE_REPO):$(EXTRA_TAG)
endif

.PHONY: staging-image-build
staging-image-build: ## Build the multi-arch staging image with buildx.
	$(IMAGE_BUILD_CMD) -t $(IMAGE_TAG) \
		--platform=$(PLATFORMS) \
		$(PUSH) \
		$(IMAGE_BUILD_EXTRA_OPTS) .

.PHONY: staging-image-push
staging-image-push: PUSH=--push ## Build and push the multi-arch staging image.
staging-image-push: staging-image-build

.PHONY: image-pushing-postsubmit
image-pushing-postsubmit: staging-image-push ## Entry point invoked by cloudbuild.yaml on merge to main.
