# TokenSpeed

The official `lightseekorg/tokenspeed:nightly-621b53` image exposes four contract
signals on the engine's `--metrics-port` endpoint. Use `tokenspeed.yaml` and one
exporter, as in the [Kubernetes example](https://github.com/kubernetes-sigs/model-serving-signals/tree/main/examples/tokenspeed).
The default `tokenspeed serve` command starts the SMG gateway and engine as
processes in the same container; the exporter runs as a sidecar in the same Pod.

| Contract signal | Native engine metric |
| --- | --- |
| `gen_ai.server.request.duration` | `tokenspeed:e2e_request_latency_seconds` |
| `gen_ai.server.time_to_first_token` | `tokenspeed:time_to_first_token_seconds` |
| `gen_ai.server.time_per_output_token` | `tokenspeed:decode_time_per_output_token_seconds` |
| `gen_ai.server.request.queued` | `tokenspeed:num_requests_waiting` |

## Select a profile for the installed version

The engine/frontend boundary changed between the versions tested here:

| Validated runtime | Engine profile | Frontend profile |
| --- | --- | --- |
| Official `nightly-621b53`: TokenSpeed `0.1.1`, SMG `1.13.0.post20261019` | `tokenspeed.yaml` provides all four signals | Not needed |
| TokenSpeed `0.1.1.post20261102`, SMG `1.13.0.post20261027` | `tokenspeed.yaml` provides queue load; native request histograms are absent | `tokenspeed-smg.yaml` provides the three latency histograms |

The SMG profile targets the latter version's long metric names. The official
image's older SMG uses `smg_router_ttft_seconds` and `smg_router_tpot_seconds`
instead. Use its native engine profile rather than the newer SMG profile.
Missing source metrics are omitted; the exporter does not invent values.

Both profiles use the same Prometheus scraper as vLLM and SGLang. Engine latency
labels include `model_name`, `app_key`, and `stream`; queue labels also include
`dp_rank`. SMG labels include `model`, `router_type`, `backend_type`, and
`endpoint`. Labels and histogram samples are preserved, with numeric bucket
boundaries serialized in Prometheus's canonical format.

## Run the engine and exporter

Install the chosen TokenSpeed version following its [getting started guide](https://lightseek.org/tokenspeed/guides/getting-started)
on supported hardware, or use the pinned official image in the Kubernetes
example. Enable the engine's native Prometheus endpoint:

```sh
tokenspeed serve Qwen/Qwen3-0.6B \
  --host 127.0.0.1 --port 8000 \
  --enable-metrics --metrics-host 127.0.0.1 --metrics-port 8001 \
  --prometheus-host 127.0.0.1 --prometheus-port 8002
```

For the official image, one exporter supplies all four signals:

```sh
go run ./cmd/exporter \
  --profile catalog/profiles/tokenspeed.yaml \
  --engine-host localhost --engine-metrics-port 8001
curl -s localhost:9464/metrics
```

For `0.1.1.post20261102` with SMG `1.13.0.post20261027`, keep that exporter for
queue load and run a second exporter for frontend latency:

```sh
go run ./cmd/exporter \
  --profile catalog/profiles/tokenspeed-smg.yaml \
  --engine-host localhost --engine-metrics-port 8002 \
  --exporter-metrics-port 9465
curl -s localhost:9465/metrics
```

The engine endpoint is distinct from the gateway's API and Prometheus ports.
Without an explicit engine metrics port, TokenSpeed allocates one and logs it.
If selecting reporters explicitly, include `--metrics-reporters prometheus`.

## Scope and semantics

The official image's native request histograms are defined in
[`RequestMetrics`](https://github.com/lightseekorg/tokenspeed/blob/621b53/python/tokenspeed/runtime/metrics/collector.py).
They measure the engine request lifecycle, including scheduler wait and prefill,
rather than HTTP transport or gateway tokenization. The TPOT mapping uses
`decode_time_per_output_token_seconds`: time from the first output event to the
last, divided by the number of subsequent output tokens. Validation used one
token per event and no speculative decoding. The similarly named
`request_time_per_output_token_seconds` includes queueing and TTFT and is not
mapped to the contract's decode TPOT. See the native
[`output processor`](https://github.com/lightseekorg/tokenspeed/blob/621b53/python/tokenspeed/runtime/engine/output_processor.py).

The waiting gauge counts the scheduler queue before dispatch. It excludes
grammar, pause, and peer-bootstrap holds preceding that queue. Scrape each node's
engine endpoint in multi-node serving and preserve the rank labels.

`gen_ai.server.request.running` remains unmapped: the native running gauge counts
admitted, unfinished requests, including the queue. The official image reached
three running requests with two queued and only one execution slot. Subtracting
the queue is not generally sufficient because other holds can precede scheduling.
See [`OutputProcessor::num_running`](https://github.com/lightseekorg/tokenspeed/blob/985e4ac3bbb0347513f67168b0820e0d3f347db1/tokenspeed-engine/crates/ts-control/src/output.rs).

The newer SMG profile was validated for regular single-node serving. It records
TTFT for streaming responses and TPOT for completed streams producing more than
one token, using `(generation_duration - TTFT) / (output_tokens - 1)`. Its
generation-duration timer includes the streaming body. The response-head timers
`smg_router_request_duration_seconds` and `smg_http_request_duration_seconds`
do not, so neither is mapped. The engine queue-wait histogram likewise cannot
provide total request duration. See
[`record_streaming_metrics`](https://github.com/smg-project/smg/blob/2596a0fe374ceb55e0198f4fef48dcf858ab42bc/model_gateway/src/observability/metrics.rs).

## Live validation

### Official image on Kubernetes

The official `nightly-621b53` image was run without package upgrades or source
patches in the gpu-ack cluster on one NVIDIA A10, serving Qwen3-0.6B in BF16 with
Triton attention/sampling, eager execution, and `--max-num-seqs 1`. Its
`linux/amd64` manifest is pinned in the example to
`sha256:f9299c4efe88f79c1da9e6d73227fdbbb38d1fa25e1ffab49b3e59eeeb9ee46a`.
The image contains TokenSpeed `0.1.1`, kernel `0.1.4.dev20261026+git00000000`,
engine `0.1.0`, SMG `1.13.0.post20261019`, and PyTorch `2.14.0+cu130`.
A registry cache was used for network access; the architecture manifest and
image layers were verified against the official image. The example retains the
official image address. Use upstream's hardware policy when selecting hardware;
this test establishes this specific A10 configuration's behavior.

Three concurrent SSE completions produced 64 tokens each, 192 in total. Each
latency histogram gained three request observations. All 109 histogram bucket,
sum, and count samples matched the native endpoint, including labels and numeric
bucket boundaries. Native decode TPOT matched the request timing calculation
within the finish-bookkeeping clock difference. Queue values followed
`0 -> 2 -> 0`, with 52 stable native/exporter comparisons matching. The engine
exporter alone provided all four contract signals.

### Newer runtime and SMG

TokenSpeed `0.1.1.post20261102`, kernel `0.1.4.post20261102`, engine
`0.1.0.post20261102`, and SMG `1.13.0.post20261027` were additionally tested in a
Debian container with GLIBC 2.41 on A10, using the same model and execution mode.
Eight concurrent requests generated 256 tokens each; queue values followed
`0 -> 7 -> 0`, with 686 stable native/exporter comparisons matching. The running
gauge reached eight while seven requests were queued.

Three further concurrent SSE requests generated 64 tokens each. The SMG latency
histograms gained three observations each; all 82 bucket, sum, and count samples
matched the frontend endpoint, and the TPOT formula matched. In the same run,
queue values followed `0 -> 2 -> 0`, with 63 stable comparisons matching. The
captured SMG exposition is retained as a regression fixture.
