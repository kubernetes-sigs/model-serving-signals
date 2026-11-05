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
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"sigs.k8s.io/model-serving-signals/pkg/profile"
)

const (
	queryRunningSignal  = "gen_ai.server.request.running"
	queryDurationSignal = "gen_ai.server.request.duration"
	queryEngineName     = "test"
	queryExposition     = `# TYPE engine:requests gauge
engine:requests{model="a",rank="0",state="running"} 3
engine:requests{model="a",rank="1",state="running"} 5
engine:requests{model="a",rank="0",state="queued"} 2
engine:requests{model="a",rank="1",state="queued"} 1
# TYPE engine:capacity gauge
engine:capacity{model="a"} 2
# TYPE engine:empty gauge
engine:empty{app_key=""} 3
`
)

func TestCollectPromQL(t *testing.T) {
	tests := []struct{ name, expression, samples string }{
		{
			name:       "label selector",
			expression: `engine:requests{state="running"}`,
			samples: `gen_ai_server_request_running{model="a",rank="0",state="running"} 3
gen_ai_server_request_running{model="a",rank="1",state="running"} 5
`,
		},
		{
			name:       "regex and negative matchers",
			expression: `engine:requests{state=~"run.*",rank!="1"}`,
			samples:    "gen_ai_server_request_running{model=\"a\",rank=\"0\",state=\"running\"} 3\n",
		},
		{
			name:       "missing label matches empty string",
			expression: `engine:requests{state="running",absent="",rank="0"}`,
			samples:    "gen_ai_server_request_running{model=\"a\",rank=\"0\",state=\"running\"} 3\n",
		},
		{
			name:       "sum aggregation",
			expression: `sum by (model) (engine:requests{state="running"})`,
			samples:    "gen_ai_server_request_running{model=\"a\"} 8\n",
		},
		{
			name:       "subtraction after label removal",
			expression: `sum without (state) (engine:requests{state="running"}) - sum without (state) (engine:requests{state="queued"})`,
			samples: `gen_ai_server_request_running{model="a",rank="0"} 1
gen_ai_server_request_running{model="a",rank="1"} 4
`,
		},
		{
			name:       "function and scalar arithmetic",
			expression: `clamp_max(sum(engine:requests{state="running"}) * 2, 10)`,
			samples:    "gen_ai_server_request_running 10\n",
		},
		{
			name:       "explicit vector matching",
			expression: `engine:requests{state="running"} / on(model) group_left engine:capacity`,
			samples: `gen_ai_server_request_running{model="a",rank="0",state="running"} 1.5
gen_ai_server_request_running{model="a",rank="1",state="running"} 2.5
`,
		},
		{
			name:       "function over classic histogram buckets",
			expression: `histogram_quantile(0.5, vllm:e2e_request_latency_seconds_bucket)`,
			samples:    "gen_ai_server_request_running{model_name=\"tiny\"} 0.75\n",
		},
		{
			name:       "vector matching normalizes empty label values",
			expression: "engine:empty - engine:empty",
			samples:    "gen_ai_server_request_running 0\n",
		},
		{name: "absent metric", expression: `engine:missing - engine:capacity`},
		{name: "unmatched labels", expression: `engine:requests - engine:capacity`},
		{name: "no historical samples", expression: `rate(engine:requests[5m])`},
		{name: "offset cannot reuse current samples", expression: `engine:requests offset 1m`},
		{name: "query type mismatch", expression: `engine:counter`},
		{name: "runtime vector matching error", expression: `engine:requests / on(model) engine:requests`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prof := &profile.Profile{Name: queryEngineName, Signals: map[string]string{queryRunningSignal: test.expression}}
			exposition := queryExposition + vllmExpositionTiny + "# TYPE engine:counter counter\nengine:counter 4\n"
			c, err := New(context.Background(), fakeScraper{exposition: exposition}, prof, testScrapeTimeout)
			require.NoError(t, err)
			want := ""
			if test.samples != "" {
				want = "# HELP gen_ai_server_request_running [ALPHA] Number of requests executing forward passes.\n" +
					"# TYPE gen_ai_server_request_running gauge\n" + test.samples
			}
			require.NoError(t, testutil.CollectAndCompare(c, strings.NewReader(want)))
		})
	}
}

func TestCollectHistogramSelector(t *testing.T) {
	exposition := vllmExpositionTiny + `vllm:e2e_request_latency_seconds_bucket{model_name="other",le="0.5"} 0
vllm:e2e_request_latency_seconds_bucket{model_name="other",le="+Inf"} 1
vllm:e2e_request_latency_seconds_count{model_name="other"} 1
vllm:e2e_request_latency_seconds_sum{model_name="other"} 0.9
`
	prof := &profile.Profile{Name: queryEngineName, Signals: map[string]string{
		queryDurationSignal: `vllm:e2e_request_latency_seconds{model_name="tiny"}`,
	}}
	c, err := New(context.Background(), fakeScraper{exposition: exposition}, prof, testScrapeTimeout)
	require.NoError(t, err)
	want := `# HELP gen_ai_server_request_duration_seconds [ALPHA] Generative AI server request duration such as time-to-last byte or last output token.
# TYPE gen_ai_server_request_duration_seconds histogram
gen_ai_server_request_duration_seconds_bucket{model_name="tiny",le="0.5"} 1
gen_ai_server_request_duration_seconds_bucket{model_name="tiny",le="1"} 2
gen_ai_server_request_duration_seconds_bucket{model_name="tiny",le="+Inf"} 3
gen_ai_server_request_duration_seconds_sum{model_name="tiny"} 4.5
gen_ai_server_request_duration_seconds_count{model_name="tiny"} 3
`
	require.NoError(t, testutil.CollectAndCompare(c, strings.NewReader(want)))
}

func TestNewRejectsInvalidPromQL(t *testing.T) {
	tests := []struct{ signal, expression, errorText string }{
		{queryRunningSignal, "sum(", "parse error"},
		{queryRunningSignal, "1 + 2", "must return an instant vector"},
		{queryRunningSignal, "engine:requests[5m]", "must return an instant vector"},
		{queryDurationSignal, "sum(engine:latency)", "histogram mappings require"},
	}
	for _, test := range tests {
		t.Run(test.expression, func(t *testing.T) {
			prof := &profile.Profile{Name: queryEngineName, Signals: map[string]string{test.signal: test.expression}}
			_, err := New(context.Background(), fakeScraper{}, prof, testScrapeTimeout)
			require.ErrorContains(t, err, test.errorText)
		})
	}
}
