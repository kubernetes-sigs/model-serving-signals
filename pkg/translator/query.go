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

package translator

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/otlptranslator"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/storage"

	"sigs.k8s.io/model-serving-signals/pkg/contract"
)

func newMapping(signal *contract.Signal, source string) (mapping, error) {
	convert, err := converterFor(signal)
	if err != nil {
		return mapping{}, err
	}
	expr, err := parser.NewParser(parser.Options{}).ParseExpr(source)
	if err != nil {
		return mapping{}, fmt.Errorf("signal '%s': %w", signal.Name, err)
	}
	if expr.Type() != parser.ValueTypeVector {
		return mapping{}, fmt.Errorf("signal '%s': mapping must return an instant vector, got %s", signal.Name, expr.Type())
	}
	for {
		paren, ok := expr.(*parser.ParenExpr)
		if !ok {
			break
		}
		expr = paren.Expr
	}
	m := mapping{signal: signal, native: source, convert: convert}
	if selector, ok := expr.(*parser.VectorSelector); ok &&
		selector.OriginalOffset == 0 && selector.OriginalOffsetExpr == nil && selector.Timestamp == nil && selector.StartOrEnd == 0 {
		m.selector = selector
	}
	if signal.Type == otlptranslator.MetricTypeHistogram && m.selector == nil {
		return mapping{}, fmt.Errorf("signal '%s': histogram mappings require a current-scrape vector selector", signal.Name)
	}
	return m, nil
}

// selectMetrics filters whole metric families, preserving classic histogram samples and types.
func (m mapping) selectMetrics(families map[string]*dto.MetricFamily) ([]prometheus.Metric, error) {
	var metrics []prometheus.Metric
	seen := make(map[string]struct{})
	for _, family := range families {
		if m.selector.Name != "" && family.GetName() != m.selector.Name {
			continue
		}
		selected := &dto.MetricFamily{Name: family.Name, Help: family.Help, Type: family.Type}
		for _, sample := range family.GetMetric() {
			ls := labelsOf(sample)
			ls[string(model.MetricNameLabel)] = family.GetName()
			if matches(m.selector.LabelMatchers, func(name string) string { return ls[name] }) {
				delete(ls, string(model.MetricNameLabel))
				if err := m.checkLabels(seen, ls); err != nil {
					return nil, err
				}
				selected.Metric = append(selected.Metric, sample)
			}
		}
		if len(selected.Metric) == 0 {
			continue
		}
		converted, err := m.convert(m.signal, selected)
		if err != nil {
			return nil, err
		}
		metrics = append(metrics, converted...)
	}
	return metrics, nil
}

func (m mapping) evaluate(ctx context.Context, engine *promql.Engine, snapshot storage.Queryable, now time.Time) ([]prometheus.Metric, error) {
	query, err := engine.NewInstantQuery(ctx, snapshot, nil, m.native, now)
	if err != nil {
		return nil, err
	}
	defer query.Close()
	vector, err := query.Exec(ctx).Vector()
	if err != nil {
		return nil, err
	}
	metrics := make([]prometheus.Metric, 0, len(vector))
	seen := make(map[string]struct{}, len(vector))
	for _, sample := range vector {
		if sample.H != nil {
			return nil, fmt.Errorf("signal '%s': gauge expression returned a histogram", m.signal.Name)
		}
		ls := sample.Metric.Map()
		delete(ls, string(model.MetricNameLabel))
		if err := m.checkLabels(seen, ls); err != nil {
			return nil, err
		}
		desc := prometheus.NewDesc(m.signal.PrometheusName, m.signal.Description, nil, ls)
		metric, err := prometheus.NewConstMetric(desc, prometheus.GaugeValue, sample.F)
		if err != nil {
			return nil, err
		}
		metrics = append(metrics, metric)
	}
	return metrics, nil
}

// checkLabels rejects series that collide when renamed to the contract metric.
func (m mapping) checkLabels(seen map[string]struct{}, ls map[string]string) error {
	key := labels.FromMap(ls).String()
	if _, exists := seen[key]; exists {
		return fmt.Errorf("signal '%s': duplicate label set %s after removing the native metric name", m.signal.Name, key)
	}
	seen[key] = struct{}{}
	return nil
}

func matches(matchers []*labels.Matcher, value func(string) string) bool {
	for _, matcher := range matchers {
		if !matcher.Matches(value(matcher.Name)) {
			return false
		}
	}
	return true
}
