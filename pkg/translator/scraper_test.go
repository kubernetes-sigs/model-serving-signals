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
	"net/http"
	"net/http/httptest"
	"testing"

	dto "github.com/prometheus/client_model/go"
)

const vllmExposition = `# HELP vllm:e2e_request_latency_seconds Histogram of e2e request latency in seconds.
# TYPE vllm:e2e_request_latency_seconds histogram
vllm:e2e_request_latency_seconds_bucket{model_name="m",le="1.0"} 2
vllm:e2e_request_latency_seconds_bucket{model_name="m",le="+Inf"} 3
vllm:e2e_request_latency_seconds_count{model_name="m"} 3
vllm:e2e_request_latency_seconds_sum{model_name="m"} 4.5
# HELP vllm:num_requests_running Number of requests currently running on GPU.
# TYPE vllm:num_requests_running gauge
vllm:num_requests_running{model_name="m"} 1
`

func TestHTTPScrape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(vllmExposition))
	}))
	defer srv.Close()

	families, err := (&HTTPScraper{url: srv.URL + "/metrics"}).Scrape(context.Background())
	if err != nil {
		t.Fatalf("Scrape: %v", err)
	}
	hist, ok := families["vllm:e2e_request_latency_seconds"]
	if !ok {
		t.Fatalf("histogram family missing; got %v", keys(families))
	}
	if hist.GetType() != dto.MetricType_HISTOGRAM {
		t.Errorf("type = %v, want HISTOGRAM", hist.GetType())
	}
	if got := hist.GetMetric()[0].GetHistogram().GetSampleCount(); got != 3 {
		t.Errorf("sample count = %d, want 3", got)
	}
	if families["vllm:num_requests_running"].GetType() != dto.MetricType_GAUGE {
		t.Error("gauge family missing or mistyped")
	}
}

func TestHTTPScrapeNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	if _, err := (&HTTPScraper{url: srv.URL}).Scrape(context.Background()); err == nil {
		t.Fatal("expected an error for a non-200 response")
	}
}

func keys(m map[string]*dto.MetricFamily) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestNewHTTPScraperURL(t *testing.T) {
	for _, tc := range []struct {
		host string
		port int
		path string
		want string
	}{
		{"localhost", 8000, "/metrics", "http://localhost:8000/metrics"},
		{"::1", 9000, "/m", "http://[::1]:9000/m"},
	} {
		if got := NewHTTPScraper(tc.host, tc.port, tc.path).(*HTTPScraper).url; got != tc.want {
			t.Errorf("NewHTTPScraper(%q, %d, %q).url = %q, want %q", tc.host, tc.port, tc.path, got, tc.want)
		}
	}
}
