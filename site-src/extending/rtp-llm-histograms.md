# RTP-LLM native histogram diagnostics

This optional tool collects native RTP-LLM latency histograms for inspection.
It does not emit model-serving-signals contract metrics and does not depend on
the `/worker_status` adapter.

## Collecting upstream latency histograms

[RTP-LLM PR #1545](https://github.com/alibaba/rtp-llm/pull/1545), tested at commit
`adc4feb851`, adds three Python KMonitor
Histogram companions. They preserve the existing Gauge samples, convert milliseconds
to seconds, and send cumulative `_bucket`, `_sum`, and `_count` components over
Flume. **The PR does not add a pull endpoint.**

The optional diagnostic
[`hack/rtp-llm/kmonitor_bridge.py`](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/hack/rtp-llm/kmonitor_bridge.py)
receives these batches and exposes native Prometheus histograms:

| Native family | Preserved timing semantics | Contract mapping |
| --- | --- | --- |
| `py_rtp_framework_rt_seconds` | Existing framework/caller timing boundaries, which vary by operation/protocol. | Unmapped until request-duration coverage is verified. |
| `py_rtp_response_first_token_rt_seconds` | First response, including control chunks/non-streaming observations; not restricted to successful requests. | Unmapped: not verified as successful-request TTFT. |
| `py_rtp_response_iterate_rt_seconds` | Per-iteration observations, with caller-specific normalization. | Unmapped: not per-request mean TPOT. |

The bridge changes the transport representation only. It does not rename these
families to `gen_ai_*`, re-bucket observations, or reinterpret their timing. The
exporter's existing Prometheus scraper can read its endpoint, but the RTP-LLM
contract profile still leaves these signals unmapped. Scrape the native endpoint
separately when examining those distributions.

Run the bridge in the same Pod/network namespace as RTP-LLM, using a Python
environment with the RTP-LLM wheel and its `thrift` dependency installed:

```sh
python3 hack/rtp-llm/kmonitor_bridge.py &
# Before starting the engine:
export HIPPO_SLAVE_IP=127.0.0.1
export HIPPO_ROLE_SHORT_NAME=rtp-llm-validation
export RTP_LLM_LATENCY_HISTOGRAM_ENABLED=1
# Start RTP-LLM, send inference requests, then:
curl -s localhost:19465/metrics
```

`RTP_LLM_LATENCY_HISTOGRAM_ENABLED=0` disables the new observations; the default
is enabled. The existing Python KMonitor reporter additionally requires a nonempty
Hippo role and sends to `HIPPO_SLAVE_IP:4141`. The bridge binds loopback by default.
It is intended for a standalone test deployment: it consumes that Flume destination
and ignores unrelated metrics, so do not substitute it for an existing production
KMonitor collector. It reuses RTP-LLM's generated Flume protocol classes.

Only complete snapshots with the pinned upstream bucket sets are published. Repeated
reports replace the previous snapshot rather than adding counts. Labels, including
`process_instance_id`, remain intact; delayed/regressing snapshots are ignored and
series expire after 30 seconds without fresh source timestamps (`--max-age`). There
is a 4096-series bound. Invalid batches are logged and rejected atomically. A changed
upstream bucket schema requires updating the bridge before use. This utility is not
part of the Go exporter image and is not a general-purpose Flume gateway.

## GPU Kubernetes example

The [diagnostic deployment](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/test/e2e/manifests/rtp-llm-histograms.yaml)
runs RTP-LLM and the bridge in the same Pod. It needs one NVIDIA GPU, a compatible
RTP-LLM runtime image with PR #1545's Python changes, and a PVC named
`rtp-llm-model` containing `Qwen3-0.6B/`. The image must provide `python3` on PATH.
The bridge disables NVIDIA device visibility and binds its endpoints to loopback.
The companion [ConfigMap manifest](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/test/e2e/manifests/rtp-llm-histograms-configmap.yaml)
provides `/bridge/kmonitor_bridge.py` for the bridge container. Apply both manifests in the same namespace.
There is no contract exporter in this diagnostic deployment. This GPU example is run manually;
`make test-e2e` continues to use the CPU simulator.

The example uses a publicly pullable Linux amd64 runtime image from Alibaba Cloud ACK's
US West registry; no registry login is required. Its tested digest is
`sha256:de4451e85a9ba0e7b2ea250dc1e4227c122b6f77f8bf58ae6ad387bad9425d1c`.
For a reproducible deployment, use
`registry-us-west-1.ack.aliyuncs.com/acs/rtp-llm@sha256:de4451e85a9ba0e7b2ea250dc1e4227c122b6f77f8bf58ae6ad387bad9425d1c`.
Model weights are mounted separately through the PVC.

```sh
export RTP_LLM_IMAGE=registry-us-west-1.ack.aliyuncs.com/acs/rtp-llm:20261026-night
kubectl create namespace rtp-llm-histogram-test
# Provision the model PVC in this namespace before applying the deployment.
kubectl -n rtp-llm-histogram-test apply \
  -f test/e2e/manifests/rtp-llm-histograms-configmap.yaml
envsubst '${RTP_LLM_IMAGE}' < test/e2e/manifests/rtp-llm-histograms.yaml | \
  kubectl -n rtp-llm-histogram-test apply -f -
kubectl -n rtp-llm-histogram-test rollout status deployment/rtp-llm-histograms --timeout=15m
kubectl -n rtp-llm-histogram-test port-forward deployment/rtp-llm-histograms \
  30000:30000 19465:19465
```

In another terminal:

```sh
curl -s localhost:30000/v1/chat/completions -H 'Content-Type: application/json' \
  -d '{"model":"default","messages":[{"role":"user","content":"Count to ten."}],"max_tokens":32,"stream":true}'
curl -s localhost:19465/metrics
```

## Updating the bridge ConfigMap

The checked-in ConfigMap embeds the bridge script so the deployment does not require a separate
`kubectl create configmap --from-file` step. After editing `hack/rtp-llm/kmonitor_bridge.py`, regenerate
it from the repository root and commit both files:

```sh
{
  echo '# Generated from hack/rtp-llm/kmonitor_bridge.py; do not edit the embedded script.'
  echo '# Regeneration instructions: site-src/extending/rtp-llm-histograms.md.'
  kubectl create configmap rtp-llm-bridge \
    --from-file=hack/rtp-llm/kmonitor_bridge.py --dry-run=client -o yaml
} > test/e2e/manifests/rtp-llm-histograms-configmap.yaml
```
