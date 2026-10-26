/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package contract defines the model-serving signal contract.
// It is the single place where contract signals are defined; all other packages import them from here.
package contract

import (
	"fmt"

	"github.com/prometheus/otlptranslator"
	"go.opentelemetry.io/otel/semconv/v1.41.0/genaiconv"
)

var (
	// Signals holds every contract signal, keyed by name. Populated in init function.
	Signals = map[string]*Signal{}

	// otelMetricNamer applies the OpenTelemetry-to-Prometheus naming rules used by Prometheus itself:
	// dots become underscores and unit/type suffixes (e.g. "_seconds", "_total") are appended.
	otelMetricNamer = otlptranslator.NewMetricNamer("", otlptranslator.UnderscoreEscapingWithSuffixes)
)

const (
	// requestCounterUnit is the OTel annotation unit for request counts.
	requestCounterUnit = "{request}"
)

// init registers the contract signals. Adding a signal to the contract means adding it here.
func init() {
	// Signals adopted verbatim from the OpenTelemetry GenAI semantic conventions.
	// semconv v1.41.0 is the last opentelemetry-go release that ships GenAI conventions; they have since moved to
	// https://github.com/open-telemetry/semantic-conventions-genai, which has no Go package yet.
	mustRegister(newSignalFromGenAiInstrument(genaiconv.ServerRequestDuration{}, otlptranslator.MetricTypeHistogram))
	mustRegister(newSignalFromGenAiInstrument(genaiconv.ServerTimeToFirstToken{}, otlptranslator.MetricTypeHistogram))
	mustRegister(newSignalFromGenAiInstrument(genaiconv.ServerTimePerOutputToken{}, otlptranslator.MetricTypeHistogram))

	// Signals defined by this project following the OpenTelemetry GenAI naming conventions until OpenTelemetry standardizes equivalents.
	mustRegister(newSignal("gen_ai.server.request.queued", requestCounterUnit, otlptranslator.MetricTypeGauge, "Number of requests admitted by the server but not yet scheduled into a batch."))
	mustRegister(newSignal("gen_ai.server.request.running", requestCounterUnit, otlptranslator.MetricTypeGauge, "Number of requests executing forward passes."))
}

// mustRegister adds a signal to Signals. It panics on error, since init has no caller to return it to.
func mustRegister(s *Signal, err error) {
	if err != nil {
		panic(err)
	}
	Signals[s.Name] = s
}

func newSignalFromGenAiInstrument(i genaiconvInstrument, t otlptranslator.MetricType) (*Signal, error) {
	return newSignal(i.Name(), i.Unit(), t, i.Description())
}

func newSignal(name string, unit string, metricType otlptranslator.MetricType, description string) (*Signal, error) {
	metric := otlptranslator.Metric{Name: name, Unit: unit, Type: metricType}
	prometheusName, err := otelMetricNamer.Build(metric)
	if err != nil {
		// Contract signal names are static and validated by tests, so this is a programming error.
		return nil, fmt.Errorf("contract signal '%s' has an invalid name: %w", name, err)
	}

	metricTypeName := typeName(metricType)

	return &Signal{
		Metric:         metric,
		Description:    description,
		PrometheusName: prometheusName,
		MetricTypeName: metricTypeName,
	}, nil
}

// typeName returns a human-readable name for an instrument type, for logs and error messages.
func typeName(t otlptranslator.MetricType) string {
	switch t {
	case otlptranslator.MetricTypeHistogram:
		return "histogram"
	case otlptranslator.MetricTypeExponentialHistogram:
		return "exponential_histogram"
	case otlptranslator.MetricTypeGauge:
		return "gauge"
	case otlptranslator.MetricTypeMonotonicCounter:
		return "counter"
	case otlptranslator.MetricTypeNonMonotonicCounter:
		return "non_monotonic_counter"
	case otlptranslator.MetricTypeSummary:
		return "summary"
	default:
		return "unknown"
	}
}

// IsSignal reports whether name is a contract signal.
func IsSignal(name string) bool {
	_, ok := Signals[name]
	return ok
}

// genaiconvInstrument is the common shape of the generated genaiconv metric types.
type genaiconvInstrument interface {
	Name() string
	Unit() string
	Description() string
}

// Signal describes one contract signal using OpenTelemetry metric semantics.
type Signal struct {
	otlptranslator.Metric
	Description    string
	PrometheusName string
	MetricTypeName string
}

// String returns a compact description of the signal for logs and error messages.
func (s *Signal) String() string {
	return fmt.Sprintf("{Name: %s, Unit: %s, Type: %s}", s.Name, s.Unit, s.MetricTypeName)
}
