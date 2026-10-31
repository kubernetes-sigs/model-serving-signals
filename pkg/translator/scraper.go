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
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"unicode/utf8"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// maxSnippetLen caps how much of an offending line is echoed into a parse error so a
// binary or oversized body cannot blow up the log line.
const maxSnippetLen = 120

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
		return nil, fmt.Errorf("reading scrape body from %s: %w", s.url, err)
	}

	// Parse decodes a Prometheus text exposition into metric families keyed by name.
	// Engine metric names such as "vllm:e2e_request_latency_seconds" contain colons, which are
	// legal under the legacy validation scheme, so UTF-8 validation is used to accept any engine's names.
	families, err := parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w; endpoint %s%s", err, s.url, offendingLine(err, body))
	}
	return families, nil
}

// offendingLine quotes the body line a text-format parse error points at, as
// `, line N: "<content>"`, so the log shows what the engine actually sent. It returns "" when err is not an
// expfmt.ParseError or its line number does not fall inside body.
func offendingLine(err error, body []byte) string {
	var perr expfmt.ParseError
	if !errors.As(err, &perr) || perr.Line < 1 {
		return ""
	}
	lines := bytes.Split(body, []byte("\n"))
	if perr.Line > len(lines) {
		return ""
	}
	// A trailing "\r" is kept on purpose: CRLF line endings are themselves a parse failure
	// (e.g. `unknown metric type "gauge\r"`), and %q renders the "\r" visibly.
	line := truncateUTF8(string(lines[perr.Line-1]), maxSnippetLen)
	return fmt.Sprintf(", line %d: %q", perr.Line, line)
}

// truncateUTF8 shortens s to at most n bytes, plus a trailing "…", without splitting a
// multi-byte character in half.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
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
