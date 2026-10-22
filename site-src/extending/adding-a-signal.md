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
| 4. Update the contract documentation | [`model-server-contract.md`](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/model-server-contract.md) | Always |

## 1. Declare the signal in the contract

Open [`pkg/contract/signals.go`](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/pkg/contract/signals.go).

**If OpenTelemetry already defines the metric** (check the
[GenAI semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-metrics/)), reuse the
generated `genaiconv` type so the name, unit and description stay in sync with the spec:

```go
var TimeToFirstToken = fromSemconv(genaiconv.ServerTimeToFirstToken{}, otlptranslator.MetricTypeHistogram)
```

**If OpenTelemetry does not define it yet**, declare it under the project-defined block following the same
naming conventions, and say what it means:

```go
var RequestQueued = Signal{
    // Unit is a UCUM unit, e.g. "s", "By", "{token}". Leave it out when dimensionless.
    Metric:      otlptranslator.Metric{Name: "gen_ai.server.request.queued", Type: otlptranslator.MetricTypeGauge},
    Description: "Number of requests admitted by the server but not yet scheduled into a batch.",
}
```

Then **add it to `Signals`**, keyed by its name. That map is the contract: profile validation, the translator and
the tests all go through it, so a signal not listed there does not exist.

Run `go test ./pkg/contract/` and extend `TestPrometheusName` with the expected Prometheus name. The name is
derived by `github.com/prometheus/otlptranslator` (dots become underscores, the unit becomes a suffix, counters
get `_total`), so the test documents what consumers will see.

## 2. Map the signal in the engine profiles

Each engine profile in [`catalog/profiles/`](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/catalog/profiles/) maps contract signal names to the engine's
native metric. The native metric is re-emitted as is under the contract name, so its instrument type must match
the signal's:

```yaml
name: vllm
signals:
  gen_ai.server.request.duration: vllm:e2e_request_latency_seconds
```

Add a line for every engine that exposes an equivalent metric. If an engine does not provide the signal,
leave it out: an absent key means "this engine does not have it" and the exporter skips it.

The exporter logs an error at scrape time when the native metric's actual type does not match the signal's.

## 3. Add a converter if the signal type is new

The translator re-emits histograms and gauges, chosen per signal by `converterFor` in
[`pkg/translator/translator.go`](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/pkg/translator/translator.go). If your signal uses another instrument type,
the exporter refuses to start with `unsupported ... signal`. To support it:

1. Add a case to `converterFor` returning a function with the signature

   ```go
   func(signal contract.Signal, family *dto.MetricFamily) ([]prometheus.Metric, error)
   ```

   Use the helpers already there: `expectType` to check the native instrument type, and `perSample` to build
   one metric per native sample with its labels carried over. `convertGauge` is the smallest example.
2. Add a test in `translator_test.go` following `TestCollectHistogram`:
   feed a fake exposition through `fakeScraper` and compare the output with `testutil.CollectAndCompare`.

## 4. Update the contract documentation

Add the signal to the table in [`model-server-contract.md`](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/model-server-contract.md), including its
standard name, instrument type, unit, and semantics. Add the corresponding native metric name for each
engine that provides it, matching the engine profiles; use `N/A` for engines that do not provide it.

## Checklist

- [ ] Signal declared in `pkg/contract/signals.go` and added to `Signals`
- [ ] `TestPrometheusName` covers the new Prometheus name
- [ ] Mapped in every `catalog/profiles/*.yaml` whose engine provides it
- [ ] The translator re-emits the signal's instrument type, or a new converter was added with a test
- [ ] Contract table in `model-server-contract.md` updated with the signal and its engine mappings
- [ ] `go test ./...` passes
