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

	"sigs.k8s.io/model-serving-signals/pkg/profile"
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

const testScrapeTimeout = 5 * time.Second

const vllmExpositionTiny = `# HELP vllm:e2e_request_latency_seconds Histogram of e2e request latency in seconds.
# TYPE vllm:e2e_request_latency_seconds histogram
vllm:e2e_request_latency_seconds_bucket{model_name="tiny",le="0.5"} 1
vllm:e2e_request_latency_seconds_bucket{model_name="tiny",le="1.0"} 2
vllm:e2e_request_latency_seconds_bucket{model_name="tiny",le="+Inf"} 3
vllm:e2e_request_latency_seconds_count{model_name="tiny"} 3
vllm:e2e_request_latency_seconds_sum{model_name="tiny"} 4.5
`

func vllmProfile(t *testing.T) *profile.Profile {
	t.Helper()
	p, err := profile.Parse([]byte(`
name: vllm
signals:
  gen_ai.server.request.duration: vllm:e2e_request_latency_seconds
`))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCollectHistogram(t *testing.T) {
	c, err := New(context.Background(), fakeScraper{exposition: vllmExpositionTiny}, vllmProfile(t), testScrapeTimeout)
	if err != nil {
		t.Fatal(err)
	}
	want := `# HELP gen_ai_server_request_duration_seconds Generative AI server request duration such as time-to-last byte or last output token.
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
	c, err := New(context.Background(), fakeScraper{err: errors.New("connection refused")}, vllmProfile(t), testScrapeTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if n := testutil.CollectAndCount(c); n != 0 {
		t.Errorf("collected %d metrics after a scrape failure, want 0", n)
	}
}
