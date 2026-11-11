# End-to-end tests

These tests verify the metrics exporter against a simulated inference engine
running on CPU, so they need no GPU and can run in presubmit CI.

## What is tested

The exporter has no engine-specific code: it scrapes an engine's native
`/metrics`, maps it through an engine profile, and re-emits the mapped
contract metrics. The e2e test proves that whole path end to end:

```
simulator /metrics (native) --scrape--> profile mapping --> exporter /metrics (contract)
```

For every signal the profile declares, the test asserts the exporter's
contract metric exactly matches the simulator's native metric that feeds it
(value for gauges; sample count, sum, and every cumulative bucket for
histograms).

Because the exporter path is identical for every engine, verifying it once
against a single simulator verifies the code for all engines. A second engine
would differ only in its profile (data), not in the code under test.

## Running locally

```sh
make test-e2e
```

Requires `docker`, `kind`, `kubectl`, and `go`. The harness
(`hack/test-e2e.sh`) recreates a dedicated kind cluster
(`model-serving-signals-e2e`) on every run, builds and loads the exporter
image, deploys the simulator + exporter pod, drives inference requests, runs
the Go assertions, and tears everything down.

Useful environment variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `E2E_KEEP_CLUSTER` | unset | keep the cluster after the run |
| `E2E_PROFILE_PATH` | `catalog/profiles/vllm.yaml` | profile used as the mapping oracle |
| `E2E_MODEL` | `Qwen/Qwen2-0.5B` | model name sent to the inference API |

## Optional real-GPU validation

The repository also includes an optional manual validation workflow for users
who have access to a GPU Kubernetes cluster. It uses the real-vLLM deployment
in [`examples/vllm/deployment.yaml`](../../examples/vllm/deployment.yaml), so it
also verifies that the example continues to work.

The workflow is not a Prow job or a required PR check. It creates a temporary
namespace in the current Kubernetes context, deploys the example, replaces the
exporter image, runs the same metric comparison as the kind test, and deletes
the namespace when it finishes.

```sh
E2E_GPU_EXPORTER_IMAGE=registry.example.com/model-serving-signals:pr-123 \
  make test-e2e-gpu
```

By default, the test uses the staging `:main` image and creates a temporary
namespace named `mss-gpu-e2e-<Unix timestamp>`. To use a different image or
namespace, set `E2E_GPU_EXPORTER_IMAGE` or `E2E_GPU_NAMESPACE`. The selected
namespace must be temporary and must not already exist because the test creates
it and deletes it when finished. The exporter image must already be pushed to
a registry that the cluster can access. The test uses the example's current
vLLM image and `Qwen/Qwen2.5-0.5B-Instruct` model.

## Adding an engine

The Go test (`e2e_test.go`) is engine-agnostic and does not need to change.
Adding an engine is a data change, and only makes sense once a CPU-only
simulator that emits the engine's native metrics exists:

1. Add the engine profile under `catalog/profiles/<engine>.yaml`.
2. Add a pod manifest under `test/e2e/manifests/<engine>.yaml` with the
   simulator and the exporter sidecar (see `vllm.yaml`).
3. Point the test at the profile via `E2E_PROFILE_PATH`.
