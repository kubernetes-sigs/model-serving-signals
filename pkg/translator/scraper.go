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

	dto "github.com/prometheus/client_model/go"

	"sigs.k8s.io/model-serving-signals/pkg/translator/prometheus"
	"sigs.k8s.io/model-serving-signals/pkg/translator/rtpllm"
)

// ScraperKind identifies a supported engine metrics protocol.
type ScraperKind string

const (
	ScraperPrometheus      ScraperKind = "prometheus"
	ScraperRTPWorkerStatus ScraperKind = "rtp-llm-worker-status"
)

// NewScraper constructs the profile-selected protocol adapter for http://host:port/path.
// An empty kind defaults to Prometheus for profiles that omit the scraper field.
func NewScraper(kind ScraperKind, host string, port int, path string) (Scraper, error) {
	switch kind {
	case "", ScraperPrometheus:
		return prometheus.New(host, port, path), nil
	case ScraperRTPWorkerStatus:
		return rtpllm.New(host, port, path), nil
	default:
		return nil, fmt.Errorf("unsupported scraper kind %q", kind)
	}
}

// Scraper fetches an engine's native metrics, keyed by metric family name.
type Scraper interface {
	Scrape(ctx context.Context) (map[string]*dto.MetricFamily, error)
}

var (
	_ Scraper = (*prometheus.Scraper)(nil)
	_ Scraper = (*rtpllm.Scraper)(nil)
)
