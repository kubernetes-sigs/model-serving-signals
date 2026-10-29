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
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
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
	httpScraper := newHTTPScraper(host, port, path)
	switch kind {
	case "", ScraperPrometheus:
		return httpScraper, nil
	case ScraperRTPWorkerStatus:
		return &RTPWorkerStatus{HTTPScraper: *httpScraper}, nil
	default:
		return nil, fmt.Errorf("unsupported scraper kind %q", kind)
	}
}

// Scraper fetches an engine's native metrics, keyed by metric family name.
type Scraper interface {
	Scrape(ctx context.Context) (map[string]*dto.MetricFamily, error)
}

// HTTPScraper scrapes a Prometheus text exposition endpoint such as vLLM's or SGLang's /metrics.
type HTTPScraper struct {
	// url is the engine's metrics endpoint, e.g. "http://localhost:8000/metrics".
	url string
}

var _ Scraper = (*HTTPScraper)(nil)

// NewHTTPScraper returns an HTTP scraper for the metrics endpoint at http://host:port/path.
func NewHTTPScraper(host string, port int, path string) Scraper {
	return newHTTPScraper(host, port, path)
}

func newHTTPScraper(host string, port int, path string) *HTTPScraper {
	u := url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(port)), Path: path}
	return &HTTPScraper{url: u.String()}
}

// Scrape fetches and parses the engine's metrics.
func (s *HTTPScraper) Scrape(ctx context.Context) (map[string]*dto.MetricFamily, error) {
	return s.scrape(ctx, string(expfmt.NewFormat(expfmt.TypeTextPlain)), parse)
}

func (s *HTTPScraper) scrape(ctx context.Context, accept string, decode func(io.Reader) (map[string]*dto.MetricFamily, error)) (map[string]*dto.MetricFamily, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return nil, fmt.Errorf("building scrape request for %s: %w", s.url, err)
	}
	req.Header.Set("Accept", accept)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("scraping %s: %w", s.url, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scraping %s: unexpected status %s", s.url, resp.Status)
	}

	return decode(resp.Body)
}

// Parse decodes a Prometheus text exposition into metric families keyed by name.
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
