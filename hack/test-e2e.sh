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

# Runs the simulation-based end-to-end test:
#   1. ensure a kind cluster exists and load the locally built exporter image;
#   2. deploy the simulator + exporter pod and wait until both are ready;
#   3. port-forward the simulator (inference + metrics) and the exporter to localhost;
#   4. run the Go e2e test, which drives inference load and asserts the exporter's
#      contract metrics match the simulator's native metrics per the engine profile.
#
# A dedicated kind cluster is recreated from scratch on every run and deleted on
# exit unless E2E_KEEP_CLUSTER is set to "true".

set -o errexit
set -o nounset
set -o pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "${REPO_ROOT}"

# Dedicated, disposable cluster; ports and timeout match the manifest and the
# Go test defaults, so they are plain constants rather than env knobs.
KIND_CLUSTER_NAME="model-serving-signals-e2e"
KUBE_CONTEXT="kind-${KIND_CLUSTER_NAME}"
MANIFEST="${REPO_ROOT}/test/e2e/manifests/vllm.yaml"
POD_NAME="mss-e2e"
# The Go test resolves the profile relative to its own source when unset; set it
# here so the harness is the single source of the default.
export E2E_PROFILE_PATH="${REPO_ROOT}/catalog/profiles/vllm.yaml"
ENGINE_PORT=8000
EXPORTER_PORT=9464
READY_TIMEOUT=120s

pf_pids=()

log() { echo "[e2e] $*"; }

cleanup() {
  local code=$?
  set +o errexit
  for pid in "${pf_pids[@]:-}"; do
    [[ -n "${pid}" ]] && kill "${pid}" >/dev/null 2>&1 || true
  done
  if [[ ${code} -ne 0 ]]; then
    log "test failed; dumping pod state"
    kubectl --context "${KUBE_CONTEXT}" describe pod "${POD_NAME}" 2>/dev/null || true
    kubectl --context "${KUBE_CONTEXT}" logs "${POD_NAME}" --all-containers --tail=50 2>/dev/null || true
  fi
  kubectl --context "${KUBE_CONTEXT}" delete -f "${MANIFEST}" --ignore-not-found --wait=false >/dev/null 2>&1 || true
  if [[ "${E2E_KEEP_CLUSTER:-}" != "true" ]]; then
    log "deleting kind cluster ${KIND_CLUSTER_NAME}"
    kind delete cluster --name "${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
  fi
  exit "${code}"
}
trap cleanup EXIT

for bin in kind kubectl go curl docker; do
  command -v "${bin}" >/dev/null 2>&1 || { log "required tool not found: ${bin}"; exit 1; }
done

log "creating kind cluster ${KIND_CLUSTER_NAME}"
kind delete cluster --name "${KIND_CLUSTER_NAME}" >/dev/null 2>&1 || true
kind create cluster --name "${KIND_CLUSTER_NAME}"

log "building and loading exporter image"
make image-load KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME}"

log "deploying ${POD_NAME}"
kubectl --context "${KUBE_CONTEXT}" apply -f "${MANIFEST}"

log "waiting for pod to be ready (timeout ${READY_TIMEOUT})"
kubectl --context "${KUBE_CONTEXT}" wait --for=condition=Ready "pod/${POD_NAME}" --timeout="${READY_TIMEOUT}"

log "port-forwarding engine :${ENGINE_PORT} and exporter :${EXPORTER_PORT}"
kubectl --context "${KUBE_CONTEXT}" port-forward "pod/${POD_NAME}" "${ENGINE_PORT}:8000" >/dev/null 2>&1 &
pf_pids+=("$!")
kubectl --context "${KUBE_CONTEXT}" port-forward "pod/${POD_NAME}" "${EXPORTER_PORT}:9464" >/dev/null 2>&1 &
pf_pids+=("$!")

# Wait for the forwards to accept connections.
for _ in $(seq 1 30); do
  if curl -sf "http://localhost:${ENGINE_PORT}/metrics" >/dev/null 2>&1 &&
     curl -sf "http://localhost:${EXPORTER_PORT}/metrics" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

log "running Go e2e assertions (the test drives inference load itself)"
go test -tags e2e -count=1 -v ./test/e2e/...

log "e2e passed"
