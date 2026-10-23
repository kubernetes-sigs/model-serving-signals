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
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"sigs.k8s.io/model-serving-signals/pkg/profile"
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

	families, err := (&HTTP{URL: srv.URL + "/metrics"}).Scrape(context.Background())
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

	if _, err := (&HTTP{URL: srv.URL}).Scrape(context.Background()); err == nil {
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
		if got := NewHTTPScraper(tc.host, tc.port, tc.path).URL; got != tc.want {
			t.Errorf("NewHTTPScraper(%q, %d, %q).URL = %q, want %q", tc.host, tc.port, tc.path, got, tc.want)
		}
	}
}

func TestScraperSelection(t *testing.T) {
	for _, tc := range []struct {
		name, declared, override string
		want                     ScraperKind
		wantErr                  bool
	}{
		{"legacy profile", "", "", ScraperPrometheus, false},
		{"explicit prometheus", string(ScraperPrometheus), string(ScraperPrometheus), ScraperPrometheus, false},
		{"RTP auto", string(ScraperRTPWorkerStatus), "", ScraperRTPWorkerStatus, false},
		{"RTP explicit", string(ScraperRTPWorkerStatus), string(ScraperRTPWorkerStatus), ScraperRTPWorkerStatus, false},
		{"vllm with RTP scraper", "", string(ScraperRTPWorkerStatus), "", true},
		{"RTP with prometheus", string(ScraperRTPWorkerStatus), string(ScraperPrometheus), "", true},
		{"unknown profile scraper", "typo", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveScraperKind(&profile.Profile{Name: tc.name, Scraper: tc.declared}, tc.override)
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("got %q, %v; want %q, error=%v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestScraperFactory(t *testing.T) {
	for _, kind := range ScraperKinds() {
		t.Run(string(kind), func(t *testing.T) {
			s, err := NewScraper(kind, "::1", 8000, "/worker_status")
			if err != nil {
				t.Fatal(err)
			}
			var endpoint string
			switch concrete := s.(type) {
			case *HTTP:
				if kind != ScraperPrometheus {
					t.Fatal("wrong adapter")
				}
				endpoint = concrete.URL
			case *RTPWorkerStatus:
				if kind != ScraperRTPWorkerStatus {
					t.Fatal("wrong adapter")
				}
				endpoint = concrete.URL
			default:
				t.Fatalf("unexpected scraper %T", s)
			}
			if endpoint != "http://[::1]:8000/worker_status" {
				t.Fatal(endpoint)
			}
		})
	}
	if _, err := NewScraper("unknown", "localhost", 8000, "/metrics"); err == nil {
		t.Fatal("unknown scraper accepted")
	}
}

// Verify the existing pull scraper understands the bridge's actual wire output.
// This is native metric collection, not a claim of contract semantic equivalence.
func TestRTPNativeHistogramScrape(t *testing.T) {
	body, err := os.ReadFile("testdata/rtp_llm_histograms.prom")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	defer srv.Close()
	families, err := (&HTTP{URL: srv.URL + "/metrics"}).Scrape(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for name, count := range map[string]uint64{
		"py_rtp_framework_rt_seconds":            6,
		"py_rtp_response_first_token_rt_seconds": 6,
		"py_rtp_response_iterate_rt_seconds":     72,
	} {
		family := families[name]
		if family == nil || family.GetType() != dto.MetricType_HISTOGRAM || len(family.Metric) != 1 {
			t.Fatalf("invalid family %s: %v", name, family)
		}
		h := family.Metric[0].GetHistogram()
		if h.GetSampleCount() != count || h.GetSampleSum() <= 0 {
			t.Fatalf("invalid histogram %s: %v", name, h)
		}
		last := h.Bucket[len(h.Bucket)-1]
		if !math.IsInf(last.GetUpperBound(), 1) || last.GetCumulativeCount() != count {
			t.Fatalf("invalid +Inf bucket: %v", last)
		}
		if labelsOf(family.Metric[0])["process_instance_id"] != "frontend-1" {
			t.Fatal("process identity lost")
		}
	}
}
