# Adding a new signal (metric)

A *signal* is one engine-agnostic metric in the contract, named with OpenTelemetry GenAI semantic conventions
(e.g. `gen_ai.server.request.duration`). The exporter re-exposes each signal in Prometheus format, with the
OTel-to-Prometheus naming rules applied automatically (`gen_ai_server_request_duration_seconds`).

This page describes the flow of adding a signal. It deliberately avoids copying code: the files it points at
are the source of truth, and each one already contains worked examples of what is described here.

## How a signal flows through the project

```
pkg/contract (declare) --> catalog/profiles (map per engine) --> pkg/translator (re-emit) --> docs (publish)
```

1. The **contract** declares the signal: its name, unit, instrument type, description and stability level.
2. Each **engine profile** maps the signal to the engine's native metric that provides it.
3. The **translator** scrapes the engine, looks each mapped signal up in the contract, and re-emits the native
   metric under the contract name.
4. The **contract documentation** tells consumers what the signal means and which engines provide it.

Adding a signal touches, at most, four places:

| Step | Where | When |
|------|-------|------|
| 1. Declare the signal | [`pkg/contract/signals.go`](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/pkg/contract/signals.go) | Always |
| 2. Map it in each engine profile | [`catalog/profiles/<engine>.yaml`](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/catalog/profiles/) | Always, for every engine that provides it |
| 3. Add a converter | [`pkg/translator/translator.go`](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/pkg/translator/translator.go) | Only if the signal uses an instrument type the translator does not re-emit yet |
| 4. Update the contract documentation | [Model server contract](../reference/contract.md) | Always |

## 1. Declare the signal in the contract

All contract signals are registered in the `init` function of
[`pkg/contract/signals.go`](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/pkg/contract/signals.go).
That registration is the contract: profile validation, the translator and the tests all look signals up
there, so a signal that is not registered does not exist.

Decide which of the two groups your signal belongs to:

- **OpenTelemetry already defines the metric.** Check the
  [GenAI semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-metrics/). If the
  metric is there, build the signal from the generated `genaiconv` instrument type, as the existing
  OpenTelemetry-derived signals do. The name, unit and description then come from the spec and cannot drift
  from it. You only choose the instrument type and the stability level.
- **OpenTelemetry does not define it yet.** Declare it in the project-defined group, following the same
  naming conventions (`gen_ai.server.*`), and give it a name, a [UCUM](https://ucum.org/) unit (for example
  `s`, `By`, `{request}`; leave it empty when dimensionless), an instrument type, a one-sentence description
  of what it measures, and a stability level.

Every new signal starts at the **Alpha** stability level. Promoting it to Beta or Stable is a separate change,
made once the signal is mapped and tested across engines. See the stability levels described in the
[Model server contract](../reference/contract.md).

The Prometheus name is derived automatically from the OpenTelemetry name, unit and type
(dots become underscores, the unit becomes a suffix, counters get `_total`), so you do not pick it; the
registration fails if the name cannot be translated.

### Lock the change into the contract tests

The contract is guarded by two tests in `pkg/contract`:

- A **golden file test** compares every registered signal against
  [`pkg/contract/testdata/signals.yaml`](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/pkg/contract/testdata/signals.yaml).
  Adding a signal fails it until you regenerate the golden file with `make update-contract-golden`. Review the
  resulting diff: it is the reviewable snapshot of what you added, including the stability prefix in the
  description.
- A **Prometheus name test** documents what consumers will see for a sample of signals. Extend it with your
  signal's expected Prometheus name, especially when the unit or type produces a suffix that is not obvious.

Run `go test ./pkg/contract/` and make sure both pass.

## 2. Map the signal in the engine profiles

Each engine profile in
[`catalog/profiles/`](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/catalog/profiles/)
maps contract signal names to the engine's native metric. The format is documented in the
[catalog README](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/catalog/README.md).

For every engine that exposes an equivalent metric, add an entry keyed by your signal's contract name whose
value is the engine's native metric name. The native metric is re-emitted as is under the contract name, so
it must have the **same meaning, unit and instrument type** as the signal: a histogram in seconds for a
histogram-in-seconds signal, a gauge for a gauge signal. Do not map a metric just because the name looks
similar.

If an engine does not provide the signal, leave it out of that profile: an absent key means "this engine does
not have it" and the exporter skips it. Profiles are validated at startup, so a typo in the signal name is
rejected as "not a contract signal", and a type mismatch between the native metric and the signal shows up in
the exporter log at scrape time as a failed conversion.

## 3. Add a converter if the signal type is new

The translator in
[`pkg/translator/translator.go`](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/pkg/translator/translator.go)
picks a converter per signal from the signal's instrument type. Today it re-emits **histograms** and
**gauges**. If your signal uses a different instrument type, the exporter refuses to start with an
`unsupported` signal error, and you need to teach it the new type:

1. Add a case for the new instrument type to the converter selection, returning a converter for it. Model the
   new converter on the existing gauge converter, which is the smallest one: it checks the native family's
   instrument type with the shared type check, then builds one contract metric per native sample with the
   shared per-sample helper, which carries the sample's labels over unchanged.
2. Add a unit test in
   [`pkg/translator/translator_test.go`](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/pkg/translator/translator_test.go)
   following the existing histogram collection test: feed a small native exposition through the fake scraper
   and compare the collector's output with the expected contract exposition.

## 4. Verify end-to-end

The end-to-end test in
[`test/e2e`](https://github.com/kubernetes-sigs/model-serving-signals/tree/main/test/e2e) iterates every
signal in the engine profile and asserts the exporter's value matches the engine's native metric. Once the
signal is mapped in the profile the test uses, `make test-e2e` covers it automatically, with no new test
code. Run it and confirm your signal shows up as a passing subtest. See the
[e2e README](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/test/e2e/README.md) for
prerequisites and knobs.

## 5. Update the contract documentation

Add the signal to the table in the [Model server contract](../reference/contract.md), including its standard
name, stability level, instrument type, unit and semantics. Add the corresponding native metric name for each
engine that provides it, matching the engine profiles; use `N/A` for engines that do not provide it.

## Checklist

- [ ] Signal registered in `pkg/contract/signals.go` with a stability level
- [ ] Golden file regenerated with `make update-contract-golden` and the diff reviewed
- [ ] Prometheus name test covers the new Prometheus name
- [ ] Mapped in every `catalog/profiles/*.yaml` whose engine provides it
- [ ] The translator re-emits the signal's instrument type, or a new converter was added with a test
- [ ] `make test-e2e` passes and the signal appears as a subtest
- [ ] Contract table in the [Model server contract](../reference/contract.md) updated with the signal and its engine mappings
- [ ] `make test` passes
