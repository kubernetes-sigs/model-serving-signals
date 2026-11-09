# End-to-end tests

CPU-only tests that verify the exporter against simulated vLLM, SGLang and
TensorRT-LLM engines. No GPU needed, so they run in presubmit CI.

## What is tested

For each engine, the test sends inference requests, then checks that every
contract metric the exporter emits exactly matches the native metric it maps
from, series by series:

```
mocker /metrics (native) --scrape--> profile mapping --> exporter /metrics (contract)
```

## Setup

One pod template ([`manifests/dynamo-mocker.yaml`](manifests/dynamo-mocker.yaml))
serves every engine:

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
loads the images; the Go test deploys, port-forwards, checks and deletes the pod
for each engine. The cluster is deleted at the end.

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

## Adding an engine

1. Add `catalog/profiles/<engine>.yaml`.
2. Map it to the mocker's `--engine-type` in `mockerEngineTypes` in
   `cluster_test.go`.
3. Add it to `defaultEngines` in `e2e_test.go`.
