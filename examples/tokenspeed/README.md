# TokenSpeed on Kubernetes

Runs the official `lightseekorg/tokenspeed:nightly-621b53` image, pinned by digest,
with Qwen3-0.6B on one NVIDIA GPU. SMG and the engine share a container; one exporter
sidecar provides all five contract signals. Use an exporter built with PromQL profile support.

From the repository root, in your target namespace:

```sh
kubectl create configmap tokenspeed-profiles \
  --from-file=catalog/profiles/tokenspeed.yaml \
  --dry-run=client -o yaml | kubectl apply -f -
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

The exporter scrapes the engine on loopback port 8001. Running requests are derived
as `tokenspeed:num_requests_running - tokenspeed:num_requests_waiting`; TPOT uses decode time after the first token.

In TokenSpeed `0.1.1.post20261102`, native latency histograms are absent. Use
[`tokenspeed-smg.yaml`](../../catalog/profiles/tokenspeed-smg.yaml) with SMG `1.13.0.post20261027`
in a second exporter scraping the gateway's `--prometheus-port`. This profile does not target the older SMG bundled in the pinned image.

Clean up with `kubectl delete -f examples/tokenspeed/` and `kubectl delete configmap tokenspeed-profiles`.
