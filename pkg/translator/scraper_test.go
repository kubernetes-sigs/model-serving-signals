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
	"testing"

	"sigs.k8s.io/model-serving-signals/pkg/translator/prometheus"
	"sigs.k8s.io/model-serving-signals/pkg/translator/rtpllm"
)

func TestScraperFactory(t *testing.T) {
	for _, kind := range []ScraperKind{"", ScraperPrometheus, ScraperRTPWorkerStatus} {
		t.Run(string(kind), func(t *testing.T) {
			s, err := NewScraper(kind, "::1", 8000, "/worker_status")
			if err != nil {
				t.Fatal(err)
			}
			var endpoint string
			switch concrete := s.(type) {
			case *prometheus.Scraper:
				if kind != "" && kind != ScraperPrometheus {
					t.Fatal("wrong adapter")
				}
				endpoint = concrete.URL
			case *rtpllm.Scraper:
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
