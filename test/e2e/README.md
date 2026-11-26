# End-to-end tests

CPU-only tests that verify the exporter against simulated inference engines. No GPU needed, so they run in presubmit CI.

## What is tested

For each engine, the test sends inference requests, then checks that every
contract metric the exporter emits exactly matches the native metric it maps
from, series by series:

```
mocker /metrics (native) --scrape--> profile mapping --> exporter /metrics (contract)
```

## Setup

One pod, built in Go by `mockerPod` in [`utils.go`](utils.go), serves every engine:

| Container | Role | Port |
| --- | --- | --- |
| `frontend` | Dynamo OpenAI-compatible frontend (inference) | 8000 |
| `engine` | [Dynamo mocker](https://docs.nvidia.com/dynamo/knowledge-base/concepts/simulation/kubernetes-mocker), emits engine-native metrics per `--engine-type` | 8081 |
| `exporter` | exporter under test | 9464 |

The image is a pinned `dynamo-planner-nightly` tag, because no stable release
includes [ai-dynamo/dynamo#17200](https://github.com/ai-dynamo/dynamo/pull/17200)
yet. That PR aligns the mocker's metrics with the real engines.

## Running

```sh
make test-e2e
```

Requires `docker`, `kind`, `kubectl`, `go`, and network access (Dynamo image,
Hugging Face tokenizer). `hack/test-e2e.sh` creates a dedicated kind cluster and
loads the images; the Go test deploys, checks and deletes the pod for each
engine, reaching its ports through the API server's port-forward. The cluster is
deleted at the end.

To rerun only the Go test against a kept cluster:

```sh
E2E_KEEP_CLUSTER=true make test-e2e
E2E_KUBE_CONTEXT=kind-model-serving-signals-e2e go test -v ./test/e2e/...
```

| Variable | Default | Purpose |
| --- | --- | --- |
| `E2E_ENGINES` | `vllm sglang trt-llm` | engines to test |
| `E2E_KEEP_CLUSTER` | unset | keep the cluster after the run |
| `E2E_KUBE_CONTEXT` | set by the harness | kube context the Go test uses; the test skips when unset |
| `E2E_DYNAMO_IMAGE` | `defaultDynamoImage` in `utils.go` | Dynamo frontend and mocker image |
| `E2E_EXPORTER_IMAGE` | `defaultExporterImage` in `utils.go` | exporter image under test |
| `E2E_PROFILE_PATH` | `catalog/profiles/vllm.yaml` | profile used as the mapping oracle |
| `E2E_MODEL` | `Qwen/Qwen2-0.5B` | model name sent to the inference API |

## Optional real-GPU validation

A manual workflow validates the exporter against a real engine on a GPU cluster you provide. It is not a Prow job or a required PR check.

```sh
make test-e2e-gpu
```

`hack/test-e2e-gpu.sh` deploys the selected example from `examples/`, replaces its exporter image, waits for the rollout, and runs `TestRealEngine`, which reuses the same comparison as the CPU suite. The namespace it creates is deleted on exit.

| Variable| Default | Purpose |
| `INFERENCE_ENGINE` | `vllm` | engine to validate: `vllm` or `tokenspeed` |
| `E2E_GPU_NAMESPACE` | `mss-gpu-e2e-<Unix timestamp>` | namespace to create; must not exist, deleted on exit |
| `E2E_GPU_KUBE_CONTEXT` | current context | kube context to deploy into |
| `E2E_GPU_EXPORTER_IMAGE` | staging `:main` image | exporter image the cluster can pull |
| `E2E_GPU_READY_TIMEOUT` | 15m | `kubectl rollout status` timeout |

## Adding an engine

1. Add `catalog/profiles/<engine>.yaml`.
2. Map it to the mocker's `--engine-type` in `mockerEngineTypes` in
   `utils.go`.
3. Add it to `defaultEngines` in `e2e_test.go`.
4. For a real GPU example, add it under `examples/<engine>/` and a case to `hack/test-e2e-gpu.sh`.
