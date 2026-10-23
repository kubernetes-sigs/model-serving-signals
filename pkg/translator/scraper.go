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
	"slices"
	"strconv"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"sigs.k8s.io/model-serving-signals/pkg/profile"
)

// ScraperKind identifies a supported engine metrics protocol.
type ScraperKind string

const (
	ScraperPrometheus      ScraperKind = "prometheus"
	ScraperRTPWorkerStatus ScraperKind = "rtp-llm-worker-status"
)

// ScraperKinds returns the supported protocols, shared by flags and validation.
func ScraperKinds() []ScraperKind {
	return []ScraperKind{ScraperPrometheus, ScraperRTPWorkerStatus}
}

// ValidateScraperKind rejects protocols the exporter cannot read.
func ValidateScraperKind(kind ScraperKind) error {
	if !slices.Contains(ScraperKinds(), kind) {
		return fmt.Errorf("unknown scraper kind %q, want one of %v", kind, ScraperKinds())
	}
	return nil
}

// ResolveScraperKind selects the profile's protocol and rejects incompatible CLI overrides.
func ResolveScraperKind(p *profile.Profile, override string) (ScraperKind, error) {
	kind := ScraperKind(p.Scraper)
	if kind == "" {
		kind = ScraperPrometheus
	}
	if err := ValidateScraperKind(kind); err != nil {
		return "", fmt.Errorf("profile %q: %w", p.Name, err)
	}
	if override != "" && ScraperKind(override) != kind {
		return "", fmt.Errorf("profile %q requires scraper %q, incompatible --engine-scraper %q", p.Name, kind, override)
	}
	return kind, nil
}

// NewScraper constructs the protocol adapter for http://host:port/path.
func NewScraper(kind ScraperKind, host string, port int, path string) (Scraper, error) {
	httpScraper := NewHTTPScraper(host, port, path)
	switch kind {
	case ScraperPrometheus:
		return httpScraper, nil
	case ScraperRTPWorkerStatus:
		return &RTPWorkerStatus{HTTP: *httpScraper}, nil
	default:
		return nil, ValidateScraperKind(kind)
	}
}

// Scraper fetches an engine's native metrics, keyed by metric family name.
type Scraper interface {
	Scrape(ctx context.Context) (map[string]*dto.MetricFamily, error)
}

// HTTP scrapes a Prometheus text exposition endpoint such as vLLM's or SGLang's /metrics.
type HTTP struct {
	// URL is the engine's metrics endpoint, e.g. "http://localhost:8000/metrics".
	URL string
	// Client is the HTTP client to use; http.DefaultClient when nil.
	Client *http.Client
}

var _ Scraper = (*HTTP)(nil)

// NewHTTPScraper returns an HTTP scraper for the metrics endpoint at http://host:port/path.
func NewHTTPScraper(host string, port int, path string) *HTTP {
	u := url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(port)), Path: path}
	return &HTTP{URL: u.String()}
}

// Scrape fetches and parses the engine's metrics.
func (h *HTTP) Scrape(ctx context.Context) (map[string]*dto.MetricFamily, error) {
	return h.scrape(ctx, string(expfmt.NewFormat(expfmt.TypeTextPlain)), parse)
}

func (h *HTTP) scrape(ctx context.Context, accept string, decode func(io.Reader) (map[string]*dto.MetricFamily, error)) (map[string]*dto.MetricFamily, error) {
	client := h.Client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("building scrape request for %s: %w", h.URL, err)
	}
	req.Header.Set("Accept", accept)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("scraping %s: %w", h.URL, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scraping %s: unexpected status %s", h.URL, resp.Status)
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
