# Adding a new engine

An *engine profile* is a YAML file that maps an inference engine's native metric names (vLLM, SGLang,
TensorRT-LLM, ...) onto the contract signals. Supporting a new engine normally requires **no Go code**: add a
profile, and the exporter can translate that engine's metrics.

## 1. Find the engine's native metrics

Start the engine and scrape its Prometheus endpoint (usually `/metrics`). For each contract signal in
[`pkg/contract/signals.go`](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/pkg/contract/signals.go), find the native metric with the same meaning **and
the same instrument type**. For example, `gen_ai.server.request.duration` is a histogram of end-to-end request
latency in seconds, so it must be mapped to a native histogram in seconds.

Do not map a metric just because the name looks similar. If the units differ, or the engine only exposes a
summary where the contract expects a histogram, leave the signal unmapped.

## 2. Create the profile

Add `catalog/profiles/<engine>.yaml`. The file name must match the `name` field:

```yaml
# Engine profile for SGLang

name: sglang
signals:
  gen_ai.server.request.duration: sglang:e2e_request_latency_seconds
  gen_ai.server.time_to_first_token: sglang:time_to_first_token_seconds
  gen_ai.server.request.queued: sglang:num_queue_reqs
```

Each entry names the native metric that is re-emitted as is under the contract signal's name. The native
instrument type must match the contract signal's: a histogram for a histogram signal, a gauge for a gauge
signal. See [Adding a new signal](adding-a-signal.md#2-map-the-signal-in-the-engine-profiles).

Rules, enforced by `profile.Parse`:

- `name` is required.
- `signals` must contain at least one entry.
- Every key must be a contract signal name; unknown keys are rejected.
- Every value is a non-empty native metric name.
- Unknown top-level fields are rejected.

A contract signal that the engine does not provide is simply omitted. The exporter logs it at debug level and
moves on.

## 3. Verify

The exporter image copies `catalog/` to `/catalog/`, so a rebuilt image serves the new profile at
`/catalog/profiles/<engine>.yaml`.

Then run the exporter against a live engine (or a saved scrape served by any static file server):

```sh
go run ./cmd/exporter --profile catalog/profiles/sglang.yaml --engine-host localhost --engine-metrics-port 30000
curl -s localhost:9464/metrics
```

Each mapped signal should appear under its contract name, e.g. `gen_ai_server_request_duration_seconds`, with
the engine's labels preserved. A type mismatch between the native metric and the contract signal shows up in
the exporter log as `Failed to convert native metric`.

## 4. Add end-to-end coverage

The end-to-end test in [`test/e2e`](https://github.com/kubernetes-sigs/model-serving-signals/tree/main/test/e2e)
drives a simulated engine and asserts the exporter's contract metrics match the engine's native metrics for
every signal in the profile. If a CPU simulator exists for your engine, add a manifest under
`test/e2e/manifests/` and wire it into the harness so the new profile is exercised in CI. Run `make test-e2e`
to confirm it passes. See the [e2e README](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/test/e2e/README.md).

## Trying a profile without rebuilding

`--profile` accepts any path, so a profile outside `catalog/` works just as well. This is handy while
iterating on a mapping, or for engines not yet in the catalog.

## When code changes are needed

- The engine exposes a signal with an instrument type the translator does not re-emit yet. See
  [Adding a new signal](adding-a-signal.md), step 3.
- The engine's metrics endpoint is not Prometheus text format. Implement the `translator.Scraper` interface in
  [`pkg/translator/`](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/pkg/translator/) and wire it in `cmd/exporter/main.go`.

## Checklist

- [ ] `catalog/profiles/<engine>.yaml` added, `name` matches the file name
- [ ] Each mapped native metric has the same meaning, unit and instrument type as the contract signal
- [ ] `go run ./cmd/exporter --profile catalog/profiles/<engine>.yaml` starts without errors
- [ ] Verified against a real scrape of the engine
- [ ] End-to-end coverage added and `make test-e2e` passes (when a simulator is available)
