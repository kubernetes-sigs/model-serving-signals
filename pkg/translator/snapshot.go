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
	"slices"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
)

// scrapeSnapshot gives PromQL a read-only view of one scrape, without a TSDB or history.
type scrapeSnapshot struct {
	series    []storage.Series
	timestamp int64
}

func newScrapeSnapshot(families map[string]*dto.MetricFamily, now time.Time) (*scrapeSnapshot, error) {
	snapshot := &scrapeSnapshot{timestamp: now.UnixMilli()}
	for _, family := range families {
		samples, err := expfmt.ExtractSamples(&expfmt.DecodeOptions{Timestamp: model.Time(snapshot.timestamp)}, family)
		if err != nil {
			return nil, err
		}
		for _, sample := range samples {
			builder := labels.NewScratchBuilder(len(sample.Metric))
			for name, value := range sample.Metric {
				builder.Add(string(name), string(value))
			}
			builder.Sort()
			chunk := chunkenc.NewXORChunk()
			appender, err := chunk.Appender()
			if err != nil {
				return nil, err
			}
			appender.Append(0, snapshot.timestamp, float64(sample.Value))
			snapshot.series = append(snapshot.series, &storage.SeriesEntry{
				Lset: builder.Labels(), SampleIteratorFn: chunk.Iterator,
			})
		}
	}
	slices.SortFunc(snapshot.series, func(a, b storage.Series) int { return labels.Compare(a.Labels(), b.Labels()) })
	return snapshot, nil
}

func (s *scrapeSnapshot) Querier(mint, maxt int64) (storage.Querier, error) {
	var series []storage.Series
	if mint <= s.timestamp && s.timestamp <= maxt {
		series = s.series
	}
	return &scrapeQuerier{Querier: storage.NoopQuerier(), series: series}, nil
}

type scrapeQuerier struct {
	storage.Querier
	series []storage.Series
}

func (q *scrapeQuerier) Select(ctx context.Context, _ bool, _ *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
	if err := ctx.Err(); err != nil {
		return storage.ErrSeriesSet(err)
	}
	set := &scrapeSeriesSet{SeriesSet: storage.EmptySeriesSet(), index: -1}
	for _, series := range q.series {
		if matches(matchers, series.Labels().Get) {
			set.series = append(set.series, series)
		}
	}
	return set
}

type scrapeSeriesSet struct {
	storage.SeriesSet
	series []storage.Series
	index  int
}

func (s *scrapeSeriesSet) Next() bool         { s.index++; return s.index < len(s.series) }
func (s *scrapeSeriesSet) At() storage.Series { return s.series[s.index] }
