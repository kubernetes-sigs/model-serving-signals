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

// Package httpmetrics provides the HTTP transport shared by metrics adapters.
package httpmetrics

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"

	dto "github.com/prometheus/client_model/go"
)

// URL builds an HTTP endpoint, including IPv6 host formatting.
func URL(host string, port int, path string) string {
	u := url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(port)), Path: path}
	return u.String()
}

// Scrape fetches an endpoint and decodes its response, honoring context cancellation.
func Scrape(ctx context.Context, endpoint string, client *http.Client, accept string, decode func(io.Reader) (map[string]*dto.MetricFamily, error)) (map[string]*dto.MetricFamily, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building scrape request for %s: %w", endpoint, err)
	}
	req.Header.Set("Accept", accept)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("scraping %s: %w", endpoint, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scraping %s: unexpected status %s", endpoint, resp.Status)
	}

	return decode(resp.Body)
}
