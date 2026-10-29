# Engine profile catalog

Each file in [`profiles/`](profiles/) is an engine profile for a model server.
A profile maps the engine's native Prometheus metrics onto the unified [model-serving signal contract](../site-src/reference/contract.md).
The metrics-exporter loads a single profile and re-emits each mapped native metric under its contract signal name, so
consumers see the same metric names and semantics regardless of the engine behind them.

## Available profiles

| Engine | Profile                                        |
|--------|------------------------------------------------|
| vLLM   | [`profiles/vllm.yaml`](profiles/vllm.yaml)     |
| SGLang | [`profiles/sglang.yaml`](profiles/sglang.yaml) |

## Profile format

```yaml
name: vllm
signals:
  # <contract signal name>: <engine's native metric name>
  gen_ai.server.request.duration: vllm:e2e_request_latency_seconds
  gen_ai.server.request.running: vllm:num_requests_running
```

- `name` identifies the engine.
- `signals` maps contract signal names to the engine's native metric names, as exposed on the engine's
  `/metrics` endpoint.
- Every key must be a signal defined in the contract; unknown fields are rejected.
- The native metric must have the same instrument type as the contract signal (for example, a histogram for
  `gen_ai.server.request.duration`); otherwise the metrics-exporter reports a mismatch and does not emit that signal.
- A contract signal that is left out of `signals` is one the engine does not provide; metrics-exporter skips it.

## Using a profile

Pass the profile to the metrics-exporter with `--profile` cmd-line arg.
The image ships this directory at `/catalog`:

```sh
--profile=/catalog/profiles/vllm.yaml
```

## Adding a profile

See [Adding a new engine](../site-src/extending/adding-an-engine.md).
