#!/bin/bash

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

# Creates a fresh kind cluster, loads the exporter and Dynamo images, then runs
# the Go e2e test, which deploys and checks each engine in E2E_ENGINES.
# The cluster is deleted on exit unless E2E_KEEP_CLUSTER=true.

set -o errexit
set -o nounset
set -o pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "${REPO_ROOT}"

KIND_CLUSTER_NAME="model-serving-signals-e2e"
KIND_CONFIG_FILE="${REPO_ROOT}/test/e2e/kind-config.yaml"
MANIFEST="${REPO_ROOT}/test/e2e/manifests/dynamo-mocker.yaml"
# The manifest is the single source for the images.
EXPORTER_IMAGE=$(awk '$1 == "image:" && $2 ~ /exporter/ { print $2; exit }' "${MANIFEST}")
DYNAMO_IMAGE=$(awk '$1 == "image:" && $2 ~ /dynamo/ { print $2; exit }' "${MANIFEST}")

log() { echo "[e2e] $*"; }

cleanup() {
  local code=$?
  if [[ "${E2E_KEEP_CLUSTER:-}" != "true" ]]; then
    log "deleting kind cluster ${KIND_CLUSTER_NAME}"
    kind delete cluster --name "${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  fi
  exit "${code}"
}
trap cleanup EXIT

for bin in kind kubectl go docker; do
  command -v "${bin}" >/dev/null 2>&1 || { log "required tool not found: ${bin}"; exit 1; }
done
[[ -n "${EXPORTER_IMAGE}" && -n "${DYNAMO_IMAGE}" ]] || { log "images not found in ${MANIFEST}"; exit 1; }

log "creating kind cluster ${KIND_CLUSTER_NAME}"
kind delete cluster --name "${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
kind create cluster --name "${KIND_CLUSTER_NAME}" --config "${KIND_CONFIG_FILE}" --wait 120s

log "building and loading exporter image ${EXPORTER_IMAGE}"
# Set IMAGE on the command line: CI (e.g. Prow) exports its own IMAGE.
make image-load KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}" IMAGE="${EXPORTER_IMAGE}"

log "pulling and loading Dynamo image ${DYNAMO_IMAGE}"
# Save only the host platform: `kind load docker-image` exports the full
# multi-arch index and fails on the platforms that weren't pulled.
dynamo_platform="linux/$(go env GOARCH)"
dynamo_archive=$(mktemp)
docker pull --platform "${dynamo_platform}" "${DYNAMO_IMAGE}"
docker save --platform "${dynamo_platform}" -o "${dynamo_archive}" "${DYNAMO_IMAGE}"
kind load image-archive "${dynamo_archive}" --name "${KIND_CLUSTER_NAME}"
rm -f "${dynamo_archive}"

# The image import can briefly stall the API server.
kubectl --context "kind-${KIND_CLUSTER_NAME}" wait --for=condition=Ready nodes --all --timeout=120s

log "running Go e2e tests"
E2E_KUBE_CONTEXT="kind-${KIND_CLUSTER_NAME}" go test -count=1 -v -timeout 45m ./test/e2e/...
