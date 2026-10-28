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

// Package prometheus scrapes engines exposing Prometheus text metrics.
package prometheus

import (
	"context"
	"fmt"
	"io"
	"net/http"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"

	"sigs.k8s.io/model-serving-signals/pkg/translator/internal/httpmetrics"
)

// Scraper reads a Prometheus text exposition endpoint such as vLLM's or SGLang's /metrics.
type Scraper struct {
	// URL is the engine's metrics endpoint.
	URL string
	// Client is the HTTP client to use; http.DefaultClient when nil.
	Client *http.Client
}

// New returns a scraper for http://host:port/path.
func New(host string, port int, path string) *Scraper {
	return &Scraper{URL: httpmetrics.URL(host, port, path)}
}

// Scrape fetches and parses the engine's metrics.
func (s *Scraper) Scrape(ctx context.Context) (map[string]*dto.MetricFamily, error) {
	return httpmetrics.Scrape(ctx, s.URL, s.Client, string(expfmt.NewFormat(expfmt.TypeTextPlain)), parse)
}

// parse decodes a Prometheus text exposition into metric families keyed by name.
// Engine metric names such as "vllm:e2e_request_latency_seconds" contain colons, which are
// legal under the legacy validation scheme, so UTF-8 validation is used to accept any engine's names.
func parse(r io.Reader) (map[string]*dto.MetricFamily, error) {
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(r)
	if err != nil {
		return nil, fmt.Errorf("parsing engine metrics: %w", err)
	}
	return families, nil
}
