# TokenSpeed

The bundled [TokenSpeed profile](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/catalog/profiles/tokenspeed.yaml)
maps the engine's native Prometheus queue gauge onto the request load signal:

| Contract signal | Native metric |
| --- | --- |
| `gen_ai.server.request.queued` | `tokenspeed:num_requests_waiting` |

The profile uses the same Prometheus scraper as vLLM and SGLang. Native labels,
including `model_name`, `app_key`, and `dp_rank` when present, pass through unchanged.

## Run the engine and exporter

Install TokenSpeed following its [getting started guide](https://lightseek.org/tokenspeed/guides/getting-started)
on supported GPU hardware. For example, launch a small model with a fixed engine
metrics port:

```sh
tokenspeed serve Qwen/Qwen3-0.6B \
  --host 127.0.0.1 --port 8000 \
  --enable-metrics --metrics-host 127.0.0.1 --metrics-port 8001
```

Run the exporter from this repository:

```sh
go run ./cmd/exporter \
  --profile catalog/profiles/tokenspeed.yaml \
  --engine-host localhost --engine-metrics-port 8001
curl -s localhost:9464/metrics
```

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

This profile leaves request duration, time to first token, and time per output
token unmapped. TokenSpeed exports client-facing latency at its frontend, not at
the engine endpoint. Its `tokenspeed:request_queue_time_seconds` histogram measures
only scheduler queue wait and cannot provide any of those three signals. See
TokenSpeed's [observability documentation](https://lightseek.org/tokenspeed/configuration/server#observability).

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
