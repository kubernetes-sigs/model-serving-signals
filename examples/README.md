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

Qwen2.5-0.5B-Instruct is ungated, so no Hugging Face token is needed. vLLM still needs a GPU node, so adjust the image, resources, and node selection for your cluster before applying.

### What the exporter emits

Scraping the exporter on `:9464` returns the contract signals mapped from vLLM's native metrics. For a model named `Qwen/Qwen2.5-0.5B-Instruct` the four vLLM-mapped signals look like this:

```
# TYPE gen_ai_server_request_duration_seconds histogram
gen_ai_server_request_duration_seconds_count{engine="0",model_name="Qwen/Qwen2.5-0.5B-Instruct"} 0
# TYPE gen_ai_server_request_queued gauge
gen_ai_server_request_queued{engine="0",model_name="Qwen/Qwen2.5-0.5B-Instruct"} 0
# TYPE gen_ai_server_request_running gauge
gen_ai_server_request_running{engine="0",model_name="Qwen/Qwen2.5-0.5B-Instruct"} 0
# TYPE gen_ai_server_time_per_output_token_seconds histogram
gen_ai_server_time_per_output_token_seconds_count{engine="0",model_name="Qwen/Qwen2.5-0.5B-Instruct"} 0
# TYPE gen_ai_server_time_to_first_token_seconds histogram
gen_ai_server_time_to_first_token_seconds_count{engine="0",model_name="Qwen/Qwen2.5-0.5B-Instruct"} 0
```

The labels (`engine`, `model_name`) carry over from the native vLLM metrics.

### Exporter image

The example uses the released exporter image `registry.k8s.io/model-serving-signals/metrics-exporter:v0.0.0`. Bump the tag to a newer release as they are published.
