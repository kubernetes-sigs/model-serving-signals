# Examples

Working examples of a model server running alongside the model-serving-signals
metrics exporter.

## vLLM + exporter sidecar

[`vllm/`](vllm/) deploys a vLLM server (Qwen2.5-0.5B-Instruct) and the metrics exporter as two containers in a single pod:

- **qwen2-5-0-5b** serves the model and exposes native Prometheus metrics on `:8000/metrics`.
- **metrics-exporter** runs as a sidecar, scrapes `localhost:8000/metrics`, maps vLLM's native metrics onto the signal contract using the bundled vLLM profile (`/catalog/profiles/vllm.yaml`, baked into the image), and re-emits them on `:9464` under engine-agnostic names.

A `Service` exposes `:9464` so a Prometheus scraper can collect the contract signals.

### Running

```sh
kubectl apply -f vllm/
```

Qwen2.5-0.5B-Instruct downloads from Hugging Face without an access request, so no Hugging Face token is needed. vLLM still needs a GPU node, so adjust the image, resources, and node selection for your cluster before applying.

### What the exporter emits

Scraping the exporter on `:9464` returns the contract signals (`gen_ai_server_*`) mapped from vLLM's native metrics, keeping the native labels such as `engine` and `model_name`. See the [signal contract](https://model-serving-signals.sigs.k8s.io/reference/contract/) for the full signal list.

### Exporter image

The example points at the staging image `us-central1-docker.pkg.dev/k8s-staging-images/model-serving-signals/metrics-exporter:main`, which tracks the main branch. On release this is swapped for the formal `registry.k8s.io` tag.
