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
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// maxSnippetLen caps how much of an offending line is echoed into a parse error so a
// binary or oversized body cannot blow up the log line.
const maxSnippetLen = 120

// lineNumRe extracts the line number from the Prometheus text parser error, whose message
// has the form "text format parsing error in line N: ...".
var lineNumRe = regexp.MustCompile(`line (\d+)`)

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
	client := h.Client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("building scrape request for %s: %w", h.URL, err)
	}
	req.Header.Set("Accept", string(expfmt.NewFormat(expfmt.TypeTextPlain)))

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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading scrape body from %s: %w", h.URL, err)
	}

	// Parse decodes a Prometheus text exposition into metric families keyed by name.
	// Engine metric names such as "vllm:e2e_request_latency_seconds" contain colons, which are
	// legal under the legacy validation scheme, so UTF-8 validation is used to accept any engine's names.
	families, err := parse(bytes.NewReader(body))
	if err != nil {
		contentType := resp.Header.Get("Content-Type")
		return nil, fmt.Errorf("%w; endpoint %s returned Content-Type %q%s", err, h.URL, contentType, offendingLine(err, body))
	}
	return families, nil
}

// offendingLine returns ", line N: <snippet>" naming the content the parser choked on, or "" if
// the parser error carries no usable line number. The snippet is truncated to maxSnippetLen.
func offendingLine(err error, body []byte) string {
	m := lineNumRe.FindStringSubmatch(err.Error())
	if m == nil {
		return ""
	}
	n, convErr := strconv.Atoi(m[1])
	if convErr != nil || n < 1 {
		return ""
	}
	lines := strings.Split(string(body), "\n")
	if n > len(lines) {
		return ""
	}
	line := strings.TrimRight(lines[n-1], "\r")
	if len(line) > maxSnippetLen {
		line = line[:maxSnippetLen] + "…"
	}
	return fmt.Sprintf(", line %d: %q", n, line)
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
