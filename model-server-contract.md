# Model server contract

The contract defines a common name, instrument type, unit, and meaning for each model-serving metric.
The table below lists every signal currently defined in [`pkg/contract/signals.go`](pkg/contract/signals.go),
with the native metric names used by the bundled engine profiles.

| Contract metric | Type | Unit | Semantics | [vLLM](catalog/profiles/vllm.yaml) | [SGLang](catalog/profiles/sglang.yaml) |
| --- | --- | --- | --- | --- | --- |
| `gen_ai.server.request.duration` | Histogram | `s` (seconds) | Request duration until the last byte or last output token. | `vllm:e2e_request_latency_seconds` | `sglang:e2e_request_latency_seconds` |
| `gen_ai.server.time_to_first_token` | Histogram | `s` (seconds) | Time to generate the first token for a successful response, including queue and prefill time. | `vllm:time_to_first_token_seconds` | `sglang:time_to_first_token_seconds` |
| `gen_ai.server.time_per_output_token` | Histogram | `s` (seconds) | Average time per output token after the first token, recorded per successful request. | `vllm:request_time_per_output_token_seconds` | `sglang:request_time_per_output_token_seconds` |
| `gen_ai.server.request.queued` | Gauge | `{request}` | Number of requests admitted by the server but not yet scheduled into a batch. | `vllm:num_requests_waiting` | `sglang:num_queue_reqs` |
| `gen_ai.server.request.running` | Gauge | `{request}` | Number of requests executing forward passes. | `vllm:num_requests_running` | `sglang:num_running_reqs` |
