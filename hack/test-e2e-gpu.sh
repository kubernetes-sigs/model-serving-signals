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
# The cluster and registry are supplied by the user; this script owns the
# temporary namespace it creates.

set -o errexit
set -o nounset
set -o pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "${REPO_ROOT}"

NAMESPACE="${E2E_GPU_NAMESPACE:-mss-gpu-e2e-$(date +%s)}"
EXPORTER_IMAGE="${E2E_GPU_EXPORTER_IMAGE:-us-central1-docker.pkg.dev/k8s-staging-images/model-serving-signals/metrics-exporter:main}"
KUBE_CONTEXT="${E2E_GPU_KUBE_CONTEXT:-$(kubectl config current-context)}"
READY_TIMEOUT="${E2E_GPU_READY_TIMEOUT:-15m}"

INFERENCE_ENGINE="$(printf '%s' "${INFERENCE_ENGINE:-vllm}" | tr '[:upper:]' '[:lower:]')"

case "${INFERENCE_ENGINE}" in
  vllm)
    MANIFEST="${REPO_ROOT}/examples/vllm"
    DEPLOYMENT="qwen2-5-0-5b"
    MODEL="Qwen/Qwen2.5-0.5B-Instruct"
    ENGINE_METRICS_PORT=8000
    ;;
  tokenspeed)
    MANIFEST="${REPO_ROOT}/examples/tokenspeed"
    DEPLOYMENT="tokenspeed"
    MODEL="mss-qwen3"
    ENGINE_METRICS_PORT=8001
    ;;
  *)
    echo "unsupported inference engine: ${INFERENCE_ENGINE} (supported: vllm, tokenspeed)" >&2
    exit 1
    ;;
esac

created_ns=false
rendered=""
log() { echo "[e2e-gpu] $*"; }

cleanup() {
  local code=$?
  set +o errexit
  [[ -n "${rendered}" ]] && rm -f "${rendered}"
  if [[ "${created_ns}" == "true" ]]; then
    log "deleting namespace ${NAMESPACE}"
    kubectl --context "${KUBE_CONTEXT}" delete namespace "${NAMESPACE}" \
      --ignore-not-found --wait=false >/dev/null 2>&1 || true
  fi
  exit "${code}"
}
trap cleanup EXIT

for bin in kubectl go; do
  command -v "${bin}" >/dev/null 2>&1 || { log "required tool not found: ${bin}"; exit 1; }
done

log "creating namespace ${NAMESPACE} in context ${KUBE_CONTEXT}"
kubectl --context "${KUBE_CONTEXT}" create namespace "${NAMESPACE}" >/dev/null
created_ns=true

log "deploying the ${INFERENCE_ENGINE} example with exporter image ${EXPORTER_IMAGE}"
rendered="$(mktemp)"
sed "s|image: us-central1-docker.pkg.dev/k8s-staging-images/model-serving-signals/metrics-exporter:main|image: ${EXPORTER_IMAGE}|" \
  "${MANIFEST}/deployment.yaml" > "${rendered}"
if ! grep -Fq "image: ${EXPORTER_IMAGE}" "${rendered}"; then
  log "exporter image was not substituted into ${MANIFEST}/deployment.yaml"
  exit 1
fi
kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" apply -f "${rendered}"
kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" apply -f "${MANIFEST}/service.yaml"

log "waiting for deployment ${DEPLOYMENT} to become ready"
kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" rollout status \
  "deployment/${DEPLOYMENT}" --timeout="${READY_TIMEOUT}"

POD_NAME="$(kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" get pods \
  -l "app=${DEPLOYMENT}" -o jsonpath='{.items[0].metadata.name}')"
if [[ -z "${POD_NAME}" ]]; then
  log "no pod found for deployment ${DEPLOYMENT}"
  exit 1
fi

log "running TestRealEngine against the real ${INFERENCE_ENGINE} server"
export E2E_KUBE_CONTEXT="${KUBE_CONTEXT}"
export E2E_NAMESPACE="${NAMESPACE}"
export E2E_REAL_ENGINE="${INFERENCE_ENGINE}"
export E2E_REAL_POD="${POD_NAME}"
export E2E_REAL_MODEL="${MODEL}"
export E2E_REAL_INFERENCE_PORT=8000
export E2E_REAL_ENGINE_METRICS_PORT="${ENGINE_METRICS_PORT}"
export E2E_REAL_EXPORTER_PORT=9464

output="$(mktemp)"
if ! go test -count=1 -v -run TestRealEngine ./test/e2e/... 2>&1 | tee "${output}"; then
  rm -f "${output}"
  exit 1
fi
if ! grep -q -- "--- PASS: TestRealEngine" "${output}"; then
  rm -f "${output}"
  log "TestRealEngine did not run; check E2E_REAL_ENGINE and E2E_KUBE_CONTEXT"
  exit 1
fi
rm -f "${output}"

log "real-GPU validation passed"