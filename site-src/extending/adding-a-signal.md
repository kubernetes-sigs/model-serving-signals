# Adding a new signal (metric)

A *signal* is one engine-agnostic metric in the contract, named with OpenTelemetry GenAI semantic conventions
(e.g. `gen_ai.server.request.duration`). The exporter re-exposes each signal in Prometheus format, with the
OTel-to-Prometheus naming rules applied automatically (`gen_ai_server_request_duration_seconds`).

Adding a signal touches, at most, four places:

| Step | Where | When |
|------|-------|------|
| 1. Declare the signal | `pkg/contract/signals.go` | Always |
| 2. Map it in each engine profile | `catalog/profiles/<engine>.yaml` | Always, for every engine that provides it |
| 3. Add a converter | `pkg/translator/translator.go` | Only if the signal uses an instrument type the translator does not re-emit yet |
| 4. Update the contract documentation | [Model server contract](../reference/contract.md) | Always |

## 1. Declare the signal in the contract

Open [`pkg/contract/signals.go`](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/pkg/contract/signals.go).

All signals are declared and registered in the `init()` function using `mustRegister`. There are two paths:

**If OpenTelemetry already defines the metric** (check the
[GenAI semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-metrics/)),
reuse the generated `genaiconv` type so the name, unit and description stay in sync with the spec:

```go
mustRegister(newSignalFromGenAiInstrument(genaiconv.ServerTimeToFirstToken{}, otlptranslator.MetricTypeHistogram, metrics.ALPHA))
```

**If OpenTelemetry does not define it yet**, declare it under the project-defined block following the same
naming conventions, and say what it means:

```go
mustRegister(newSignal("gen_ai.server.request.queued", requestCounterUnit, otlptranslator.MetricTypeGauge, "Number of requests admitted by the server but not yet scheduled into a batch.", metrics.ALPHA))
```

The unit argument follows OpenTelemetry conventions: annotation units such as `requestCounterUnit`
(`"{request}"`) do not add a Prometheus suffix, while time units such as `"s"` add `_seconds`.

Every new signal starts at `metrics.ALPHA` (from `k8s.io/component-base/metrics`). Don't put the
stability level in the description yourself: `newSignal` already prefixes it with `[ALPHA]` (following
the Kubernetes HELP-text convention), so writing it by hand produces `[ALPHA] [ALPHA] ...`. Promoting
the signal to `metrics.BETA` or `metrics.STABLE` is a separate change, made once the signal is mapped
and tested across engines.

`mustRegister` adds the signal to the `Signals` map keyed by its name. That map is the contract:
profile validation, the translator and the tests all go through it, so a signal not listed there does not
exist.

Run `go test ./pkg/contract/` and extend `TestPrometheusName` with the expected Prometheus name. If the
golden snapshot changed, regenerate it with `make update-contract-golden` and commit the updated
`pkg/contract/testdata/signals.yaml`; review the diff, since that file is the reviewed snapshot of the
contract. The name is derived by `github.com/prometheus/otlptranslator` (dots become underscores, the
unit becomes a suffix, counters get `_total`), so the test documents what consumers will see.

## 2. Map the signal in the engine profiles

Each engine profile in [`catalog/profiles/`](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/catalog/profiles/) maps contract signals to PromQL over the current scrape.
For a direct selector, including a bare metric name, the native instrument type must match the signal.
Histogram selectors preserve buckets, count and sum. A gauge mapping can also compute its value from
several samples, as in the running-request example below.

```yaml
name: vllm
signals:
  gen_ai.server.request.duration: vllm:e2e_request_latency_seconds # histogram, seconds
  gen_ai.server.request.running: sum by (model_name) (vllm:num_requests_running)
```

Add a line for every engine that exposes an equivalent metric. If an engine does not provide the signal,
leave it out: an absent key means "this engine does not have it" and the exporter skips it.

The exporter logs direct-selector type mismatches at scrape time. Gauge expressions must return an instant
vector of float samples. The exporter rejects range selectors, subqueries, time offsets and `@` modifiers
when it loads the mappings.

## 3. Add a converter if the signal type is new

The translator re-emits histograms and gauges, chosen per signal by `converterFor` in
[`pkg/translator/translator.go`](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/pkg/translator/translator.go). If your signal uses another instrument type,
the exporter refuses to start with `unsupported ... signal`. To support it:

1. Add a case to `converterFor` returning a function with the signature

   ```go
   func(signal *contract.Signal, family *dto.MetricFamily) ([]prometheus.Metric, error)
   ```

   Use the helpers already there: `expectType` to check the native instrument type, and `perSample` to build
   one metric per native sample with its labels carried over. `convertGauge` is the smallest example.
2. Add a test in `translator_test.go` following `TestCollectHistogram`:
   feed a fake exposition through `fakeScraper` and compare the output with `testutil.CollectAndCompare`.

## 4. Verify end-to-end

The end-to-end test in [`test/e2e`](https://github.com/kubernetes-sigs/model-serving-signals/tree/main/test/e2e)
iterates every signal in the engine profile and asserts the exporter's value matches the engine's native
metric by name. Run `make test-e2e` for bare metric names in the simulator profiles. For selectors with
label filters or computed gauges, verify the expected samples against a real or saved engine scrape.

## 5. Update the contract documentation

Add the signal to the table in the [Model server contract](../reference/contract.md), including its
standard name, stability level, instrument type, unit, and semantics. Add the corresponding native metric name for each
engine that provides it, matching the engine profiles; use `N/A` for engines that do not provide it.

## Checklist

- [ ] Signal declared in `pkg/contract/signals.go` `init()` via `mustRegister(newSignalFromGenAiInstrument(...))` or `mustRegister(newSignal(...))` with a stability level
- [ ] `TestPrometheusName` covers the new Prometheus name
- [ ] Mapped in every `catalog/profiles/*.yaml` whose engine provides it
- [ ] The translator re-emits the signal's instrument type, or a new converter was added with a test
- [ ] `make test-e2e` passes and the signal appears as a subtest
- [ ] Contract table in the [Model server contract](../reference/contract.md) updated with the signal and its engine mappings
- [ ] `go test ./...` passes
