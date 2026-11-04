# TokenSpeed on Kubernetes

This example runs the official `lightseekorg/tokenspeed:nightly-621b53` image,
pinned to its `linux/amd64` manifest digest. The default `tokenspeed serve`
command starts both the SMG gateway and the inference engine in the same
container. One exporter sidecar provides all five contract metrics from the engine's
native Prometheus endpoint: queued requests, running requests, request duration, TTFT, and TPOT.

The deployment requests one NVIDIA GPU and downloads the public
`Qwen/Qwen3-0.6B` model to an ephemeral cache. Allow enough disk space for the
image and model, and check TokenSpeed's [hardware requirements](https://github.com/lightseekorg/tokenspeed)
for your GPU. Triton attention/sampling and eager execution are selected here;
this example is intended for checking metrics, not benchmarking throughput.
The first startup can include JIT kernel compilation; the startup probe allows
up to 15 minutes before restarting the engine.

Run these commands from the repository root, in the namespace where you want to
deploy the example:

```sh
kubectl create configmap tokenspeed-profiles \
  --from-file=catalog/profiles/tokenspeed.yaml \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f examples/tokenspeed/deployment.yaml
kubectl apply -f examples/tokenspeed/service.yaml
kubectl rollout status deployment/tokenspeed --timeout=15m
kubectl port-forward service/tokenspeed 8000:8000 9464:9464
```

The profile is mounted from a ConfigMap. Use an exporter build that supports gauge
subtraction, as implemented in this repository; older builds that only rename metrics
cannot evaluate the running expression.

In another terminal, generate a streaming response with more than one output
token:

```sh
curl -N http://localhost:8000/v1/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"mss-qwen3","prompt":"Count from 1 to 100:","stream":true,"max_tokens":64,"temperature":0}'
curl -s http://localhost:9464/metrics
```

All five contract metrics are available on port 9464. The native engine metrics
bind to loopback port 8001 inside the Pod. TPOT uses the engine's decode-time
histogram, which excludes the first token and queueing time. This pinned image
does not need a gateway metrics exporter.

In TokenSpeed `0.1.1.post20261102`, native request histograms are absent: the
engine profile provides queued and derived running request counts. With SMG `1.13.0.post20261027`, use
[`tokenspeed-smg.yaml`](../../catalog/profiles/tokenspeed-smg.yaml) in a second
exporter scraping the gateway's `--prometheus-port` for latency. That profile
targets the newer SMG metric names, not the older SMG bundled in this image.

`gen_ai.server.request.running` is derived as
`tokenspeed:num_requests_running - tokenspeed:num_requests_waiting`, because the native
running gauge includes queued requests. With one execution slot, the official image
reported a native running count of three and a waiting count of two, yielding one
executing request. The exporter matches both gauges by their complete label sets.

To remove the example:

```sh
kubectl delete -f examples/tokenspeed/service.yaml
kubectl delete -f examples/tokenspeed/deployment.yaml
kubectl delete configmap tokenspeed-profiles
```
