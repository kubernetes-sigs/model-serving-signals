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
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"sigs.k8s.io/model-serving-signals/pkg/profile"
)

const (
	testScrapeTimeout = 5 * time.Second
	testRunningSignal = "gen_ai.server.request.running"
	testDifference    = "test:total - test:waiting"
	testEngineName    = "test"

	vllmExpositionTiny = `# HELP vllm:e2e_request_latency_seconds Histogram of e2e request latency in seconds.
# TYPE vllm:e2e_request_latency_seconds histogram
vllm:e2e_request_latency_seconds_bucket{model_name="tiny",le="0.5"} 1
vllm:e2e_request_latency_seconds_bucket{model_name="tiny",le="1.0"} 2
vllm:e2e_request_latency_seconds_bucket{model_name="tiny",le="+Inf"} 3
vllm:e2e_request_latency_seconds_count{model_name="tiny"} 3
vllm:e2e_request_latency_seconds_sum{model_name="tiny"} 4.5
`
)

var (
	vllmProfile = &profile.Profile{
		Name: "vllm",
		Signals: map[string]string{
			"gen_ai.server.request.duration": "vllm:e2e_request_latency_seconds",
		},
	}
)

// fakeScraper returns a fixed exposition, as an engine would.
type fakeScraper struct {
	exposition string
	err        error
}

func (f fakeScraper) Scrape(context.Context) (map[string]*dto.MetricFamily, error) {
	if f.err != nil {
		return nil, f.err
	}
	return parse(strings.NewReader(f.exposition))
}

func TestCollectHistogram(t *testing.T) {

	c, err := New(context.Background(), fakeScraper{exposition: vllmExpositionTiny}, vllmProfile, testScrapeTimeout)
	if err != nil {
		t.Fatal(err)
	}
	want := `# HELP gen_ai_server_request_duration_seconds [ALPHA] Generative AI server request duration such as time-to-last byte or last output token.
# TYPE gen_ai_server_request_duration_seconds histogram
gen_ai_server_request_duration_seconds_bucket{model_name="tiny",le="0.5"} 1
gen_ai_server_request_duration_seconds_bucket{model_name="tiny",le="1"} 2
gen_ai_server_request_duration_seconds_bucket{model_name="tiny",le="+Inf"} 3
gen_ai_server_request_duration_seconds_sum{model_name="tiny"} 4.5
gen_ai_server_request_duration_seconds_count{model_name="tiny"} 3
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(want)); err != nil {
		t.Fatal(err)
	}
}

func TestCollectScrapeErrorYieldsNothing(t *testing.T) {
	c, err := New(context.Background(), fakeScraper{err: errors.New("connection refused")}, vllmProfile, testScrapeTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if n := testutil.CollectAndCount(c); n != 0 {
		t.Errorf("collected %d metrics after a scrape failure, want 0", n)
	}
}

func TestCollectGaugeDifference(t *testing.T) {
	tests := []struct {
		name        string
		exposition  string
		wantSamples string
	}{
		{
			name: "match complete labels regardless of series or label order",
			exposition: `# TYPE test:total gauge
test:total{model="a",rank="0"} 3
test:total{model="a",rank="1"} 5
test:total{model="b",rank="0"} 4
# TYPE test:waiting gauge
test:waiting{rank="0",model="b"} 4
test:waiting{rank="1",model="a"} 2
test:waiting{rank="0",model="a"} 2
`,
			wantSamples: `gen_ai_server_request_running{model="a",rank="0"} 1
gen_ai_server_request_running{model="a",rank="1"} 3
gen_ai_server_request_running{model="b",rank="0"} 0
`,
		},
		{
			name: "omit unmatched series rather than assuming zero",
			exposition: `# TYPE test:total gauge
test:total{model="a"} 3
test:total{model="b"} 5
test:total{model="a",rank="0"} 7
# TYPE test:waiting gauge
test:waiting{model="a"} 2
test:waiting{model="c"} 2
`,
			wantSamples: "gen_ai_server_request_running{model=\"a\"} 1\n",
		},
		{
			name: "preserve signed subtraction for unlabelled gauges",
			exposition: `# TYPE test:total gauge
test:total 1
# TYPE test:waiting gauge
test:waiting 2
`,
			wantSamples: "gen_ai_server_request_running -1\n",
		},
		{
			name: "missing left metric",
			exposition: `# TYPE test:waiting gauge
test:waiting 2
`,
		},
		{
			name: "missing right metric",
			exposition: `# TYPE test:total gauge
test:total 3
`,
		},
		{
			name: "left metric must be a gauge",
			exposition: `# TYPE test:total counter
test:total 3
# TYPE test:waiting gauge
test:waiting 2
`,
		},
		{
			name: "right metric must be a gauge",
			exposition: `# TYPE test:total gauge
test:total 3
# TYPE test:waiting counter
test:waiting 2
`,
		},
		{
			name: "duplicate labels on right",
			exposition: `# TYPE test:total gauge
test:total 3
# TYPE test:waiting gauge
test:waiting 2
test:waiting 1
`,
		},
		{
			name: "duplicate labels on left",
			exposition: `# TYPE test:total gauge
test:total 3
test:total 4
# TYPE test:waiting gauge
test:waiting 2
`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prof := &profile.Profile{Name: testEngineName, Signals: map[string]string{testRunningSignal: testDifference}}
			c, err := New(context.Background(), fakeScraper{exposition: test.exposition}, prof, testScrapeTimeout)
			require.NoError(t, err)
			want := ""
			if test.wantSamples != "" {
				want = "# HELP gen_ai_server_request_running [ALPHA] Number of requests executing forward passes.\n" +
					"# TYPE gen_ai_server_request_running gauge\n" + test.wantSamples
			}
			require.NoError(t, testutil.CollectAndCompare(c, strings.NewReader(want)))
		})
	}
}

func TestNewRejectsUnsupportedExpressions(t *testing.T) {
	for _, source := range []string{
		"test:total + test:waiting",
		"test:total -",
		"test:total - test:waiting - test:other",
		"test:total{model=\"a\"} - test:waiting",
	} {
		t.Run(source, func(t *testing.T) {
			prof := &profile.Profile{Name: testEngineName, Signals: map[string]string{testRunningSignal: source}}
			_, err := New(context.Background(), fakeScraper{}, prof, testScrapeTimeout)
			require.ErrorContains(t, err, "expected a native metric name or 'metric - metric'")
		})
	}
	prof := &profile.Profile{Name: testEngineName, Signals: map[string]string{
		"gen_ai.server.request.duration": testDifference,
	}}
	_, err := New(context.Background(), fakeScraper{}, prof, testScrapeTimeout)
	require.ErrorContains(t, err, "subtraction is supported only for gauge signals")
}
