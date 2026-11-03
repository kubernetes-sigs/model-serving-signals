# TokenSpeed

TokenSpeed exposes its scheduler load and its SMG frontend latency on separate
Prometheus endpoints. Two bundled profiles cover four contract signals:

| Contract signal | Native metric | Profile / endpoint |
| --- | --- | --- |
| `gen_ai.server.request.queued` | `tokenspeed:num_requests_waiting` | `tokenspeed.yaml` / engine `--metrics-port` |
| `gen_ai.server.request.duration` | `smg_router_generation_duration_seconds` | `tokenspeed-smg.yaml` / frontend `--prometheus-port` |
| `gen_ai.server.time_to_first_token` | `smg_router_time_to_first_token_seconds` | `tokenspeed-smg.yaml` / frontend `--prometheus-port` |
| `gen_ai.server.time_per_output_token` | `smg_router_request_time_per_output_token_seconds` | `tokenspeed-smg.yaml` / frontend `--prometheus-port` |

Both profiles use the same Prometheus scraper as vLLM and SGLang. Native labels
pass through unchanged: the engine uses `model_name`, `app_key`, and `dp_rank`;
the latency series use `model`, `router_type`, `backend_type`, and `endpoint`.

## Run the engine and exporter

Install TokenSpeed following its [getting started guide](https://lightseek.org/tokenspeed/guides/getting-started)
on supported GPU hardware. For example, launch a small model with a fixed engine
metrics port:

```sh
tokenspeed serve Qwen/Qwen3-0.6B \
  --host 127.0.0.1 --port 8000 \
  --enable-metrics --metrics-host 127.0.0.1 --metrics-port 8001 \
  --prometheus-host 127.0.0.1 --prometheus-port 8002
```

Run an exporter for each endpoint, in separate terminals:

```sh
go run ./cmd/exporter \
  --profile catalog/profiles/tokenspeed.yaml \
  --engine-host localhost --engine-metrics-port 8001
curl -s localhost:9464/metrics
```

```sh
go run ./cmd/exporter \
  --profile catalog/profiles/tokenspeed-smg.yaml \
  --engine-host localhost --engine-metrics-port 8002 \
  --exporter-metrics-port 9465
curl -s localhost:9465/metrics
```

The first exporter emits queued requests. The second emits the three latency
histograms. Both sets use the contract names and retain their source labels.

With the default Prometheus reporter, the engine's launcher serves `/metrics` on
`--metrics-port`. Without an explicit port, TokenSpeed allocates one and logs its
address. Use this engine endpoint for the profile. The gateway's API port and its
separate Prometheus port do not expose these engine metrics. When selecting
reporters explicitly, include `--metrics-reporters prometheus`.

## Scope and semantics

The mapping was checked against TokenSpeed commit
[`985e4ac3bbb0347513f67168b0820e0d3f347db1`](https://github.com/lightseekorg/tokenspeed/tree/985e4ac3bbb0347513f67168b0820e0d3f347db1),
particularly the definitions in
[`EngineMetrics`](https://github.com/lightseekorg/tokenspeed/blob/985e4ac3bbb0347513f67168b0820e0d3f347db1/python/tokenspeed/runtime/metrics/collector.py)
and the scheduler snapshots in the
[`event loop`](https://github.com/lightseekorg/tokenspeed/blob/985e4ac3bbb0347513f67168b0820e0d3f347db1/tokenspeed-engine/crates/ts-control/src/event_loop.rs).

The waiting gauge counts requests in the scheduler queue, sampled before dispatch.
In prefill/decode disaggregation, the waiting gauge excludes requests still
bootstrapping with a peer; it does not include all frontend or admission waits.
Scrape each node's engine endpoint in a multi-node deployment, since the launcher
aggregates only node-local processes. Preserve the rank labels when interpreting
the resulting series.

`gen_ai.server.request.running` is also unmapped in this version. Despite its name,
`tokenspeed:num_requests_running` counts all admitted, unfinished requests in
the output processor, including requests still waiting to be scheduled. It is
not the contract's count of requests executing forward passes. See
[`OutputProcessor::num_running`](https://github.com/lightseekorg/tokenspeed/blob/985e4ac3bbb0347513f67168b0820e0d3f347db1/tokenspeed-engine/crates/ts-control/src/output.rs)
and the event loop's admission path. Subtracting the waiting gauge is not generally
sufficient, since grammar, pause, and peer-bootstrap holds can precede the scheduler queue.

The SMG profile provides the latency signals for regular, single-node serving.
TTFT is recorded for streaming responses, and TPOT is recorded once per completed
stream that produces more than one output token. SMG computes TPOT as
`(generation_duration - TTFT) / (output_tokens - 1)`. Non-streaming responses
provide generation duration without TTFT or TPOT. Generate streaming traffic
before expecting all three histograms to appear.

The duration mapping uses `smg_router_generation_duration_seconds`, measured
through completion of the generated response. The similarly named
`smg_router_request_duration_seconds` and `smg_http_request_duration_seconds`
stop when the streaming response head is returned, so neither is mapped.
The engine's `tokenspeed:request_queue_time_seconds` measures only scheduler
queue wait and also cannot provide the request duration. See the
[SMG metrics reference](https://lightseek.org/smg/main/reference/metrics) and
[`record_streaming_metrics`](https://github.com/smg-project/smg/blob/2596a0fe374ceb55e0198f4fef48dcf858ab42bc/model_gateway/src/observability/metrics.rs).

## Live validation

Validated with TokenSpeed `0.1.1.post20261102`, kernel
`0.1.4.post20261102`, and engine `0.1.0.post20261102`, serving
Qwen3-0.6B in BF16 on one NVIDIA A10. The test used Triton attention and
sampling, eager execution, and `--max-num-seqs 1` in a Debian container with
GLIBC 2.41. Use TokenSpeed's upstream hardware support policy when choosing
deployment hardware.

All eight concurrent completion requests succeeded, generating 256 tokens each.
The native queue gauge rose from zero to seven and returned to zero after the
requests completed. Across 686 stable scrape comparisons, the exporter preserved
the queue values and the `app_key`, `dp_rank`, and `model_name` labels. A comparison
was considered stable when the native queue values immediately before and after
the exporter scrape agreed.

With one execution slot, the native running gauge reached eight while seven
requests were queued. This also confirmed the semantic difference described above.

The SMG frontend profile was additionally validated with
`tokenspeed-smg 1.13.0.post20261027`: three concurrent SSE completions generated
64 tokens each. Each latency histogram gained three observations, and all 82
bucket, sum, and count samples matched the native endpoint, including labels.
The per-request TPOT formula above also matched the recorded observations.
During the same run the engine queue rose to two, then returned to zero; 63
stable comparisons matched the queue exporter. The captured latency exposition
is used by the regression test in `pkg/translator/testdata/tokenspeed-smg.prom`.
