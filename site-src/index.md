# Model Serving Signals

Engine-agnostic signal contract for model servers on Kubernetes: common metric
semantics, engine profiles (e.g., vLLM, SGLang, TensorRT-LLM), a mapping exporter,
a conformance suite, and autoscaling integration.

## Model-serving signal contract

See the [model server contract](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/model-server-contract.md)
for the complete metric table, including standard names, types, units, semantics,
and engine-specific metric mappings.

## Design

The motivation, scope, and design of this project are described in the
[model-serving-signals proposal doc](https://docs.google.com/document/d/1o2JCgOT5qBQ0x3NsAga5AAFr9BfGjMEPWmcP0NomHgE/edit).

## Extending

- [Adding a new signal (metric)](extending/adding-a-signal.md)
- [Adding a new engine](extending/adding-an-engine.md)

## Community, discussion, contribution, and support

Learn how to engage with the Kubernetes community on the
[community page](http://kubernetes.io/community/).

You can reach the maintainers of this project at:

- [Slack channel](https://kubernetes.slack.com/messages/model-serving-signals)
- [Mailing List](https://groups.google.com/a/kubernetes.io/g/sig-autoscaling)

### Code of conduct

Participation in the Kubernetes community is governed by the
[Kubernetes Code of Conduct](https://github.com/kubernetes-sigs/model-serving-signals/blob/main/code-of-conduct.md).
