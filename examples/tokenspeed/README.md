# TokenSpeed on Kubernetes

This example runs the official `lightseekorg/tokenspeed:nightly-621b53` image,
pinned to its `linux/amd64` manifest digest. The default `tokenspeed serve`
command starts both the SMG gateway and the inference engine in the same
container. One exporter sidecar translates all four metrics from the engine's
native Prometheus endpoint: queue, request duration, TTFT, and TPOT.

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

The profile is mounted from a ConfigMap, so this example also works with
exporter images that do not yet bundle it.

In another terminal, generate a streaming response with more than one output
token:

```sh
curl -N http://localhost:8000/v1/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"mss-qwen3","prompt":"Count from 1 to 100:","stream":true,"max_tokens":64,"temperature":0}'
curl -s http://localhost:9464/metrics
```

All four contract metrics are available on port 9464. The native engine metrics
bind to loopback port 8001 inside the Pod. TPOT uses the engine's decode-time
histogram, which excludes the first token and queueing time. This pinned image
does not need a gateway metrics exporter. For other versions and the remaining
running-request gap, see the [TokenSpeed profile documentation](../../site-src/extending/tokenspeed.md).

To remove the example:

```sh
kubectl delete -f examples/tokenspeed/service.yaml
kubectl delete -f examples/tokenspeed/deployment.yaml
kubectl delete configmap tokenspeed-profiles
```
