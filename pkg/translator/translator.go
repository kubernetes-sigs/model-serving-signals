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

// Package translator converts an engine's native metrics into contract signals.
//
// The Collector scrapes the engine on every Prometheus collection, looks up each contract signal's
// native metric in the engine profile, and re-emits that metric family under the contract name. The
// native metric must have the same instrument type as the contract signal; the converter for each
// signal type is chosen by converterFor. Supporting a new engine means adding a profile, not code.
package translator

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/otlptranslator"

	"sigs.k8s.io/model-serving-signals/pkg/contract"
	"sigs.k8s.io/model-serving-signals/pkg/profile"
)

// converter turns one native metric family into the Prometheus metrics for a contract signal.
type converter func(signal *contract.Signal, family *dto.MetricFamily) ([]prometheus.Metric, error)

// converterFor resolves the converter for a contract signal from its instrument type.
func converterFor(signal *contract.Signal) (converter, error) {
	switch signal.Type {
	case otlptranslator.MetricTypeHistogram:
		return convertHistogram, nil
	case otlptranslator.MetricTypeGauge:
		return convertGauge, nil
	}
	return nil, fmt.Errorf("unsupported signal %s", signal)
}

// mapping binds a contract signal to the engine's native metric that provides it.
type mapping struct {
	signal  *contract.Signal
	native  string
	convert converter
}

// Collector is a prometheus.Collector that exposes an engine's metrics as contract signals.
type Collector struct {
	// ctx is the base context every scrape derives from. prometheus.Collector.Collect takes no context,
	// so the process-wide one is captured at construction; cancelling it aborts in-flight scrapes.
	ctx      context.Context
	scraper  Scraper
	mappings []mapping
	timeout  time.Duration
	logger   logr.Logger
}

var _ prometheus.Collector = (*Collector)(nil)

// Option configures a Collector.
type Option func(*Collector)

// WithLogger sets the logger used to report collection problems.
func WithLogger(l logr.Logger) Option { return func(c *Collector) { c.logger = l } }

// New builds a Collector that resolves every contract signal through the given profile. Scrapes run
// under ctx, bounded per collection by timeout; cancelling ctx stops them.
// It fails if the profile maps a signal whose type the translator cannot emit.
func New(ctx context.Context, scraper Scraper, engineProfile *profile.Profile, timeout time.Duration, opts ...Option) (*Collector, error) {
	c := &Collector{ctx: ctx, scraper: scraper, timeout: timeout, logger: logr.Discard()}
	for _, opt := range opts {
		opt(c)
	}
	var errs []error
	for _, signal := range contract.Signals {
		native, ok := engineProfile.Metric(signal)
		if !ok {
			c.logger.V(1).Info("Engine does not provide contract signal", "engine", engineProfile.Name, "signal", signal.Name)
			continue
		}
		convert, err := converterFor(signal)
		if err != nil {
			errs = append(errs, fmt.Errorf("profile '%s': %w", engineProfile.Name, err))
			continue
		}
		c.mappings = append(c.mappings, mapping{signal: signal, native: native, convert: convert})
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return c, nil
}

// Describe sends nothing, making this an unchecked collector: the set of metrics depends on
// what the engine exposes at scrape time, so it cannot be declared up front.
func (c *Collector) Describe(chan<- *prometheus.Desc) {}

// Collect scrapes the engine and emits the contract signals it provides.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(c.ctx, c.timeout)
	defer cancel()

	families, err := c.scraper.Scrape(ctx)
	if err != nil {
		c.logger.Error(err, "Failed to scrape engine metrics")
		return
	}
	for _, m := range c.mappings {
		family, ok := families[m.native]
		if !ok {
			// Engines register some metrics lazily (e.g. after the first request), so this is expected early on.
			c.logger.V(1).Info("Native metric not present in scrape", "signal", m.signal.Name, "native", m.native)
			continue
		}
		metrics, err := m.convert(m.signal, family)
		if err != nil {
			c.logger.Error(err, "Failed to convert native metric", "signal", m.signal.Name, "native", m.native)
			continue
		}
		for _, metric := range metrics {
			ch <- metric
		}
	}
}

// convertHistogram re-emits a native Prometheus histogram under the contract signal's name,
// preserving the native label set and bucket boundaries.
func convertHistogram(signal *contract.Signal, family *dto.MetricFamily) ([]prometheus.Metric, error) {
	if err := expectType(signal, family, dto.MetricType_HISTOGRAM); err != nil {
		return nil, err
	}
	return perSample(signal, family, func(desc *prometheus.Desc, m *dto.Metric) (prometheus.Metric, error) {
		h := m.GetHistogram()
		if h == nil {
			return nil, fmt.Errorf("native metric %q has a sample without histogram data", family.GetName())
		}
		buckets := make(map[float64]uint64, len(h.GetBucket()))
		for _, b := range h.GetBucket() {
			// The +Inf bucket is implicit in NewConstHistogram (equal to the sample count).
			if b.GetUpperBound() == math.Inf(1) {
				continue
			}
			buckets[b.GetUpperBound()] = b.GetCumulativeCount()
		}
		return prometheus.NewConstHistogram(desc, h.GetSampleCount(), h.GetSampleSum(), buckets)
	})
}

// convertGauge re-emits a native gauge under the contract signal's name.
func convertGauge(signal *contract.Signal, family *dto.MetricFamily) ([]prometheus.Metric, error) {
	if err := expectType(signal, family, dto.MetricType_GAUGE); err != nil {
		return nil, err
	}
	return perSample(signal, family, func(desc *prometheus.Desc, m *dto.Metric) (prometheus.Metric, error) {
		if m.GetGauge() == nil {
			return nil, fmt.Errorf("native metric '%s' has a sample without gauge data", family.GetName())
		}
		return prometheus.NewConstMetric(desc, prometheus.GaugeValue, m.GetGauge().GetValue())
	})
}

// expectType fails when the native metric family is not one of the accepted instrument types.
func expectType(signal *contract.Signal, family *dto.MetricFamily, accepted ...dto.MetricType) error {
	got := family.GetType()
	for _, t := range accepted {
		if got == t {
			return nil
		}
	}
	return fmt.Errorf("native metric '%s' is a '%s', which cannot provide '%s' signal '%s'",
		family.GetName(), got, signal.MetricTypeName, signal.Name)
}

// perSample builds one contract metric per native sample, carrying the sample's labels over unchanged.
func perSample(signal *contract.Signal, family *dto.MetricFamily,
	build func(desc *prometheus.Desc, m *dto.Metric) (prometheus.Metric, error)) ([]prometheus.Metric, error) {
	metrics := make([]prometheus.Metric, 0, len(family.GetMetric()))
	for _, m := range family.GetMetric() {
		desc := prometheus.NewDesc(signal.PrometheusName, signal.Description, nil, labelsOf(m))
		metric, err := build(desc, m)
		if err != nil {
			return nil, fmt.Errorf("building '%s': %w", signal.Name, err)
		}
		metrics = append(metrics, metric)
	}
	return metrics, nil
}

// labelsOf returns a native sample's labels as Prometheus const labels.
// Labels are passed through unchanged; mapping engine label names onto contract attributes
// (e.g. model_name -> gen_ai.request.model) is a profile concern to be added later.
func labelsOf(m *dto.Metric) prometheus.Labels {
	labels := make(prometheus.Labels, len(m.GetLabel()))
	for _, l := range m.GetLabel() {
		labels[l.GetName()] = l.GetValue()
	}
	return labels
}
