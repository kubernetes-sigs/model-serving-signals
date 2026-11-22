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
	"bufio"
	"bytes"
	"context"
	"errors"
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

	lines := &scrapeLines{body: bufio.NewReader(resp.Body)}
	families, err := parse(lines)
	if err != nil {
		return nil, fmt.Errorf("%w (endpoint %s, Content-Type %q%s)",
			err, s.url, resp.Header.Get("Content-Type"), lines.offendingLine(err))
	}
	return families, nil
}

const maxOffendingLineLen = 200

// scrapeLines feeds the parser at most one line per read so that its buffering cannot
// advance past the line it reports. Long lines are delivered in chunks, in full.
type scrapeLines struct {
	body    *bufio.Reader
	pending []byte // Unconsumed part of the last ReadSlice result.
	readErr error  // Returned after pending bytes have been delivered.
	line    int
	prefix  []byte // First maxOffendingLineLen+1 bytes of the current line.
}

func (r *scrapeLines) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.pending) == 0 {
		// ErrBufferFull means the next chunk continues the same line.
		continuation := r.readErr == bufio.ErrBufferFull
		if r.readErr != nil && !continuation {
			return 0, r.readErr
		}
		r.pending, r.readErr = r.body.ReadSlice('\n')
		if len(r.pending) == 0 {
			return 0, r.readErr
		}
		if !continuation {
			r.line++
			line := bytes.TrimSuffix(r.pending, []byte{'\n'})
			r.prefix = append(r.prefix[:0], line[:min(len(line), maxOffendingLineLen+1)]...)
		}
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func (r *scrapeLines) offendingLine(err error) string {
	var parseErr expfmt.ParseError
	if !errors.As(err, &parseErr) || parseErr.Line != r.line {
		return ""
	}
	line := string(r.prefix)
	if len(line) > maxOffendingLineLen {
		line = line[:maxOffendingLineLen] + "..."
	}
	return fmt.Sprintf(", line %d: %q", r.line, line)
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
