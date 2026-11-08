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
	u := url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(port)), Path: path}
	return &HTTPScraper{url: u.String()}
}

// Scrape fetches and parses the engine's metrics.
func (s *HTTPScraper) Scrape(ctx context.Context) (map[string]*dto.MetricFamily, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return nil, fmt.Errorf("building scrape request for %s: %w", s.url, err)
	}
	req.Header.Set("Accept", string(expfmt.NewFormat(expfmt.TypeTextPlain)))

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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading scrape response from %s: %w", s.url, err)
	}
	families, err := parse(bytes.NewReader(body))
	if err != nil {
		// The text parser only reports a line number. Name the endpoint, what it said it was
		// returning, and the offending content so the failure can be diagnosed from the log alone.
		return nil, fmt.Errorf("%w (endpoint %s, Content-Type %q%s)",
			err, s.url, resp.Header.Get("Content-Type"), offendingLine(err, body))
	}
	return families, nil
}

// maxOffendingLineLen caps how much of a bad line is echoed into an error, so a binary or
// otherwise oversized body does not flood the log.
const maxOffendingLineLen = 200

// parseErrorLine matches the line number in the Prometheus text parser's errors, which read
// "text format parsing error in line N: ...".
var parseErrorLine = regexp.MustCompile(`parsing error in line (\d+)`)

// offendingLine returns ", line N: <content>" for the line the text parser failed on, or "" when
// the error carries no line number or the line is out of range. Trailing "\r" is kept: CRLF line
// endings are themselves a parse failure, and quoting makes them visible.
func offendingLine(err error, body []byte) string {
	m := parseErrorLine.FindStringSubmatch(err.Error())
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
	line := lines[n-1]
	if len(line) > maxOffendingLineLen {
		line = line[:maxOffendingLineLen] + "..."
	}
	return fmt.Sprintf(", line %d: %q", n, line)
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
