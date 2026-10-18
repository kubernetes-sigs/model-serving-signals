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

// Signal describes one contract signal using OpenTelemetry metric semantics.
type Signal struct {
	otlptranslator.Metric
	Description string
}

// PrometheusName returns the signal's name in Prometheus exposition format.
func (s Signal) PrometheusName() string {
	// namer applies the OpenTelemetry-to-Prometheus naming rules used by Prometheus itself:
	// dots become underscores and unit/type suffixes (e.g. "_seconds", "_total") are appended.
	namer := otlptranslator.NewMetricNamer("", otlptranslator.UnderscoreEscapingWithSuffixes)

	name, err := namer.Build(s.Metric)
	if err != nil {
		// Contract signal names are static and validated by tests, so this is a programming error.
		panic(fmt.Sprintf("contract signal %q has an invalid name: %v", s.Name, err))
	}
	return name
}

// Signals adopted verbatim from the OpenTelemetry GenAI semantic conventions.
// semconv v1.41.0 is the last opentelemetry-go release that ships GenAI conventions; they have since moved to
// https://github.com/open-telemetry/semantic-conventions-genai, which has no Go package yet.
var (
	RequestDuration    = fromSemconv(genaiconv.ServerRequestDuration{}, otlptranslator.MetricTypeHistogram)
	TimeToFirstToken   = fromSemconv(genaiconv.ServerTimeToFirstToken{}, otlptranslator.MetricTypeHistogram)
	TimePerOutputToken = fromSemconv(genaiconv.ServerTimePerOutputToken{}, otlptranslator.MetricTypeHistogram)
)

// Signals defined by this project following the OpenTelemetry GenAI naming conventions,
// until OpenTelemetry standardizes equivalents.
var (
	RequestQueued = Signal{
		Metric:      otlptranslator.Metric{Name: "gen_ai.server.request.queued", Type: otlptranslator.MetricTypeGauge},
		Description: "Number of requests admitted by the server but not yet scheduled into a batch.",
	}
	RequestRunning = Signal{
		Metric:      otlptranslator.Metric{Name: "gen_ai.server.request.running", Type: otlptranslator.MetricTypeGauge},
		Description: "Number of requests executing forward passes.",
	}
)

// Signals holds every contract signal, keyed by name. Adding a signal to the contract means adding it here.
var Signals = map[string]Signal{
	RequestDuration.Name:    RequestDuration,
	TimeToFirstToken.Name:   TimeToFirstToken,
	TimePerOutputToken.Name: TimePerOutputToken,
	RequestQueued.Name:      RequestQueued,
	RequestRunning.Name:     RequestRunning,
}

// Lookup returns the contract signal with the given name.
func Lookup(name string) (Signal, bool) {
	s, ok := Signals[name]
	return s, ok
}

// IsSignal reports whether name is a contract signal.
func IsSignal(name string) bool {
	_, ok := Signals[name]
	return ok
}

// semconvInstrument is the common shape of the generated genaiconv metric types.
type semconvInstrument interface {
	Name() string
	Unit() string
	Description() string
}

func fromSemconv(i semconvInstrument, t otlptranslator.MetricType) Signal {
	return Signal{
		Metric:      otlptranslator.Metric{Name: i.Name(), Unit: i.Unit(), Type: t},
		Description: i.Description(),
	}
}

// TypeName returns a human-readable name for an instrument type, for logs and error messages.
func TypeName(t otlptranslator.MetricType) string {
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
