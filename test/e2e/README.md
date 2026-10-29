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

## Adding an engine

The Go test (`e2e_test.go`) is engine-agnostic and does not need to change.
Adding an engine is a data change, and only makes sense once a CPU-only
simulator that emits the engine's native metrics exists:

1. Add the engine profile under `catalog/profiles/<engine>.yaml`.
2. Add a pod manifest under `test/e2e/manifests/<engine>.yaml` with the
   simulator and the exporter sidecar (see `vllm.yaml`).
3. Point the test at the profile via `E2E_PROFILE_PATH`.

## Manual GPU diagnostics

`manifests/rtp-llm-histograms.yaml` and `manifests/rtp-llm-histograms-configmap.yaml`
provide the RTP-LLM engine, histogram bridge, and its script ConfigMap for manual GPU testing.
They are not applied by `make test-e2e` and do not expose the standard contract metrics.
See the [RTP-LLM histogram guide](../../site-src/extending/rtp-llm-histograms.md) for image,
model PVC, deployment, and inference instructions.
