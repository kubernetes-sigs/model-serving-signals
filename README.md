# model-serving-signals

Engine-agnostic signal contract for model servers on Kubernetes: common metric semantics, engine profiles (e.g., vLLM, SGLang, TensorRT-LLM), a mapping exporter, a conformance suite, and autoscaling integration.

## Sponsoring SIGs

This project is sponsored by:

- Kubernetes [SIG Autoscaling](https://github.com/kubernetes/community/tree/master/sig-autoscaling)
- Kubernetes [SIG Instrumentation](https://github.com/kubernetes/community/tree/master/sig-instrumentation)
- OpenTelemetry [GenAI SIG](https://github.com/open-telemetry/community/blob/main/projects/gen-ai.md)

## Model-serving signal contract

See the [model server contract](site-src/reference/contract.md) for the complete metric table,
including standard names, types, units, semantics, and engine-specific metric mappings.

## Design

The motivation, scope, and design of this project are described in the [model-serving-signals proposal doc](https://docs.google.com/document/d/1o2JCgOT5qBQ0x3NsAga5AAFr9BfGjMEPWmcP0NomHgE/edit).

## Documentation

The project website is built from [`site-src/`](site-src/) with MkDocs and
published via Netlify. `site-src/` is the single source of truth for docs;
build locally with `make build-docs` (or `make live-docs` to preview).

## Extending

- [Adding a new signal (metric)](site-src/extending/adding-a-signal.md)
- [Adding a new engine](site-src/extending/adding-an-engine.md)

## Community, discussion, contribution, and support

Learn how to engage with the Kubernetes community on the [community page](http://kubernetes.io/community/).

You can reach the maintainers of this project at:

- [Slack channel](https://kubernetes.slack.com/messages/model-serving-signals)
- [Mailing List](https://groups.google.com/a/kubernetes.io/g/sig-autoscaling)

### Code of conduct

Participation in the Kubernetes community is governed by the [Kubernetes Code of Conduct](code-of-conduct.md).
