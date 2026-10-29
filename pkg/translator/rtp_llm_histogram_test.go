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
)

// Verify the existing pull scraper understands the bridge's actual wire output.
// This is native metric collection, not a claim of contract semantic equivalence.
func TestRTPNativeHistogramScrape(t *testing.T) {
	body, err := os.ReadFile("testdata/rtp_llm_histograms.prom")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	defer srv.Close()
	families, err := (&HTTPScraper{URL: srv.URL + "/metrics"}).Scrape(context.Background())
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
