# TokenSpeed on Kubernetes

Runs the official `lightseekorg/tokenspeed:nightly-621b53` image, pinned by digest,
with Qwen3-0.6B on one NVIDIA GPU (linux/amd64). One exporter sidecar provides all five contract signals.
The exporter image must include this profile and [PromQL support (#68)](https://github.com/kubernetes-sigs/model-serving-signals/pull/68);
until both are merged, build an image containing both changes and override the exporter image in the manifest.

From the repository root, in your target namespace:

```sh
kubectl apply -f examples/tokenspeed/deployment.yaml
kubectl apply -f examples/tokenspeed/service.yaml
kubectl rollout status deployment/tokenspeed --timeout=15m
kubectl port-forward service/tokenspeed 8000:8000 9464:9464
```

In another terminal:

```sh
curl -N http://localhost:8000/v1/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"mss-qwen3","prompt":"Count from 1 to 100:","stream":true,"max_tokens":64,"temperature":0}'
curl -s http://localhost:9464/metrics
```

`--disable-kvstore` disables the host-memory KV cache to avoid exceeding the container memory limit.
Triton sampling avoids an incompatible FlashInfer kernel in this image on NVIDIA A10 GPUs.
The exporter scrapes the engine on loopback port 8001. Running requests are derived
as `tokenspeed:num_requests_running - tokenspeed:num_requests_waiting`; TPOT uses decode time after the first token.

Clean up with `kubectl delete -f examples/tokenspeed/`.
