# Model server contract

The contract defines a common name, instrument type, unit, meaning, and stability level for each model-serving metric.
The table below lists every signal currently defined in [`pkg/contract/signals.go`](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/pkg/contract/signals.go),
with the native metric names used by the bundled engine profiles.

Each signal has a stability level, using the [Kubernetes metric stability levels](https://kubernetes.io/docs/reference/using-api/deprecation-policy/#deprecating-a-metric):

- **Alpha**: the signal has no stability guarantees; it may change or be removed at any time.
- **Beta**: changes and removal go through the Kubernetes metrics deprecation policy.
- **Stable**: the signal will not change; removal goes through the Kubernetes metrics deprecation policy.

Each signal's description starts with its stability level, e.g. `[ALPHA] Number of requests executing forward passes.`, following the Kubernetes metrics convention.

| Contract metric | Stability | Type | Unit | Semantics | [vLLM](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/catalog/profiles/vllm.yaml) | [SGLang](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/catalog/profiles/sglang.yaml) | [TensorRT-LLM](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/catalog/profiles/trt-llm.yaml) | [TokenSpeed](../extending/tokenspeed.md) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `gen_ai.server.request.duration` | Alpha | Histogram | `s` (seconds) | Request duration until the last byte or last output token. | `vllm:e2e_request_latency_seconds` | `sglang:e2e_request_latency_seconds` | `trtllm_e2e_request_latency_seconds` | `tokenspeed:e2e_request_latency_seconds` (engine) / `smg_router_generation_duration_seconds` (SMG) |
| `gen_ai.server.time_to_first_token` | Alpha | Histogram | `s` (seconds) | Time to generate the first token for a successful response, including queue and prefill time. | `vllm:time_to_first_token_seconds` | `sglang:time_to_first_token_seconds` | `trtllm_time_to_first_token_seconds` | `tokenspeed:time_to_first_token_seconds` (engine) / `smg_router_time_to_first_token_seconds` (SMG) |
| `gen_ai.server.time_per_output_token` | Alpha | Histogram | `s` (seconds) | Average time per output token after the first token, recorded per successful request. | `vllm:request_time_per_output_token_seconds` | `sglang:request_time_per_output_token_seconds` | `trtllm_time_per_output_token_seconds` | `tokenspeed:decode_time_per_output_token_seconds` (engine) / `smg_router_request_time_per_output_token_seconds` (SMG) |
| `gen_ai.server.request.queued` | Alpha | Gauge | `{request}` | Number of requests admitted by the server but not yet scheduled into a batch. | `vllm:num_requests_waiting` | `sglang:num_queue_reqs` | `trtllm_num_requests_waiting` | `tokenspeed:num_requests_waiting` |
| `gen_ai.server.request.running` | Alpha | Gauge | `{request}` | Number of requests executing forward passes. | `vllm:num_requests_running` | `sglang:num_running_reqs` | `trtllm_num_requests_running` | Not mapped (different semantics) |

The official TokenSpeed `nightly-621b53` image provides four signals with the
single engine profile, `tokenspeed.yaml`. In `0.1.1.post20261102`, native request
histograms are absent; use the engine profile for queue load and
`tokenspeed-smg.yaml` with SMG `1.13.0.post20261027` for latency. The SMG mapping
requires streaming responses for TTFT/TPOT and was validated for regular
single-node serving. See the [TokenSpeed guide](../extending/tokenspeed.md).
