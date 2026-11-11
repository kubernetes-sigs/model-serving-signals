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

# Runs the optional real-GPU validation against an existing Kubernetes cluster.
# The cluster and registry are supplied by the user; this script only owns the
# temporary namespace it creates.

set -o errexit
set -o nounset
set -o pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "${REPO_ROOT}"

NAMESPACE="${E2E_GPU_NAMESPACE:-mss-gpu-e2e-$(date +%s)}"
EXPORTER_IMAGE="${E2E_GPU_EXPORTER_IMAGE:-us-central1-docker.pkg.dev/k8s-staging-images/model-serving-signals/metrics-exporter:main}"
MANIFEST="${REPO_ROOT}/examples/vllm"
DEPLOYMENT="qwen2-5-0-5b"
ENGINE_PORT=8000
EXPORTER_PORT=9464
READY_TIMEOUT="${E2E_GPU_READY_TIMEOUT:-15m}"

pf_pids=()

log() { echo "[e2e-gpu] $*"; }

wait_for_metrics() {
  for _ in $(seq 1 30); do
    if curl -sf "http://localhost:${ENGINE_PORT}/metrics" >/dev/null 2>&1 &&
       curl -sf "http://localhost:${EXPORTER_PORT}/metrics" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

cleanup() {
  local code=$?
  set +o errexit
  for pid in "${pf_pids[@]:-}"; do
    [[ -n "${pid}" ]] && kill "${pid}" >/dev/null 2>&1 || true
  done
  kubectl delete namespace "${NAMESPACE}" --ignore-not-found --wait=false >/dev/null 2>&1 || true
  exit "${code}"
}
trap cleanup EXIT

for bin in kubectl go curl; do
  command -v "${bin}" >/dev/null 2>&1 || { log "required tool not found: ${bin}"; exit 1; }
done

log "creating namespace ${NAMESPACE}"
kubectl create namespace "${NAMESPACE}" >/dev/null

log "deploying the vLLM example with exporter image ${EXPORTER_IMAGE}"
sed "s|image: us-central1-docker.pkg.dev/k8s-staging-images/model-serving-signals/metrics-exporter:main|image: ${EXPORTER_IMAGE}|" \
  "${MANIFEST}/deployment.yaml" | kubectl -n "${NAMESPACE}" apply -f -
kubectl -n "${NAMESPACE}" apply -f "${MANIFEST}/service.yaml"
kubectl -n "${NAMESPACE}" wait \
  --for=condition=Ready \
  pod -l "app=${DEPLOYMENT}" \
  --timeout="${READY_TIMEOUT}"

POD_NAME=$(kubectl -n "${NAMESPACE}" get pods \
  -l "app=${DEPLOYMENT}" \
  -o jsonpath='{.items[0].metadata.name}')

log "port-forwarding vLLM and exporter metrics"
kubectl -n "${NAMESPACE}" port-forward "pod/${POD_NAME}" \
  "${ENGINE_PORT}:8000" >/dev/null 2>&1 &
pf_pids+=("$!")
kubectl -n "${NAMESPACE}" port-forward "pod/${POD_NAME}" \
  "${EXPORTER_PORT}:9464" >/dev/null 2>&1 &
pf_pids+=("$!")

if ! wait_for_metrics; then
  log "timed out waiting for vLLM and exporter metrics"
  kubectl -n "${NAMESPACE}" logs "pod/${POD_NAME}" --all-containers --tail=100 || true
  exit 1
fi

log "running metric comparison against the real vLLM server"
E2E_MODEL="Qwen/Qwen2.5-0.5B-Instruct" \
  go test -count=1 -v ./test/e2e/...

log "real-GPU validation passed"
