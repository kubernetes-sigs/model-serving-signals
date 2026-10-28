//go:build e2e

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

// Package e2e verifies the metrics exporter end to end against a simulated
// inference engine. The cluster lifecycle (kind, deploy, port-forward) is owned
// by hack/test-e2e.sh; this test assumes the simulator's native metrics endpoint,
// the exporter's contract endpoint, and the simulator's inference API are already
// reachable (typically via port-forward to localhost) and provided through the
// environment.
//
// The test drives its own inference load so it controls timing: it first lets a
// batch of requests complete (populating histograms), then holds one request
// in-flight so num_requests_running is non-zero while both endpoints are scraped.
// This gives the gauge assertions real teeth rather than comparing 0 to 0.
//
// The test is engine-agnostic: the engine profile is the only source of truth
// for which native metric maps to which contract signal, so adding an engine
// means adding a profile and a deployment, not changing this code.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"

	"sigs.k8s.io/model-serving-signals/pkg/contract"
	"sigs.k8s.io/model-serving-signals/pkg/profile"
	"sigs.k8s.io/model-serving-signals/pkg/translator/prometheus"
)

// These match the local kind + port-forward setup in hack/test-e2e.sh and the
// values baked into test/e2e/manifests/vllm.yaml. They are constants, not env
// knobs, because nothing varies them across runs. Only the profile path is
// configurable, so a new engine can point the test at its own mapping oracle.
const (
	model         = "Qwen/Qwen2-0.5B"
	engineHost    = "localhost"
	enginePort    = 8000
	exporterHost  = "localhost"
	exporterPort  = 9464
	metricsPath   = "/metrics"
	warmupCount   = 5
	scrapeTimeout = 5 * time.Second
)

// defaultProfilePath resolves the vLLM profile relative to this test file, so
// the default works regardless of the working directory `go test` runs from
// (it chdirs to the package directory).
func defaultProfilePath() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "catalog/profiles/vllm.yaml"
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "catalog", "profiles", "vllm.yaml")
}

func profilePath() string {
	if v := os.Getenv("E2E_PROFILE_PATH"); v != "" {
		return v
	}
	return defaultProfilePath()
}

// TestExporterMatchesEngine proves the full scrape -> map -> emit path: for every
// signal the profile declares, the exporter's contract metric must exactly match
// the engine's native metric that feeds it, including its label set.
func TestExporterMatchesEngine(t *testing.T) {
	path := profilePath()
	prof, err := profile.Load(path)
	if err != nil {
		t.Fatalf("load profile %q: %v", path, err)
	}
	if len(prof.Signals) == 0 {
		t.Fatal("profile declares no signals")
	}

	engineScraper := prometheus.New(engineHost, enginePort, metricsPath)
	exporterScraper := prometheus.New(exporterHost, exporterPort, metricsPath)
	completionsURL := (&url.URL{Scheme: "http", Host: fmt.Sprintf("%s:%d", engineHost, enginePort), Path: "/v1/completions"}).String()

	// Phase 1: let a batch of requests complete so histograms are populated.
	for i := 0; i < warmupCount; i++ {
		if err := infer(context.Background(), completionsURL, model, 16); err != nil {
			t.Fatalf("warmup inference request: %v", err)
		}
	}

	// Phase 2: hold one request in-flight so num_requests_running > 0, then scrape
	// both endpoints as close together as possible. A wide gap, or scraping while
	// idle, would make the gauge comparison meaningless.
	var wg sync.WaitGroup
	wg.Add(1)
	inflightCtx, cancelInflight := context.WithCancel(context.Background())
	defer func() { cancelInflight(); wg.Wait() }()
	go func() {
		defer wg.Done()
		// A large max_tokens combined with the simulator's inter-token latency keeps
		// this request running long enough to be observed.
		_ = infer(inflightCtx, completionsURL, model, 256)
	}()

	const runningSignal = "gen_ai.server.request.running"
	runningNative, ok := prof.Signals[runningSignal]
	if !ok {
		t.Fatalf("profile does not map %q; cannot gate on in-flight requests", runningSignal)
	}
	waitForRunning(t, engineScraper, runningNative, scrapeTimeout)

	ctx, cancel := context.WithTimeout(context.Background(), scrapeTimeout)
	defer cancel()
	native, err := engineScraper.Scrape(ctx)
	if err != nil {
		t.Fatalf("scrape engine %s: %v", engineScraper.URL, err)
	}
	emitted, err := exporterScraper.Scrape(ctx)
	if err != nil {
		t.Fatalf("scrape exporter %s: %v", exporterScraper.URL, err)
	}

	for signalName, nativeName := range prof.Signals {
		signal, ok := contract.Signals[signalName]
		if !ok {
			t.Errorf("profile maps %q which is not a contract signal", signalName)
			continue
		}
		contractName := signal.PrometheusName

		t.Run(signalName, func(t *testing.T) {
			nativeFam, ok := native[nativeName]
			if !ok {
				t.Fatalf("engine did not emit native metric %q (did the inference requests reach it?)", nativeName)
			}
			contractFam, ok := emitted[contractName]
			if !ok {
				t.Fatalf("exporter did not emit contract metric %q", contractName)
			}
			compareFamily(t, signal, nativeFam, contractFam)
			compareLabels(t, nativeFam, contractFam)
		})
	}
}

// waitForRunning polls the engine until the given native gauge is > 0, so the
// in-flight request is actually observable before the test scrapes.
func waitForRunning(t *testing.T, scraper *prometheus.Scraper, nativeName string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		families, err := scraper.Scrape(ctx)
		cancel()
		if err == nil {
			if fam, ok := families[nativeName]; ok && gaugeTotal(fam) > 0 {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q > 0; no request stayed in-flight", nativeName)
}

// infer sends a single completion request to the simulator.
func infer(ctx context.Context, completionsURL, model string, maxTokens int) error {
	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"prompt":     "hello",
		"max_tokens": maxTokens,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, completionsURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s from %s", resp.Status, completionsURL)
	}
	return nil
}

// compareFamily asserts the emitted contract family equals the native family,
// dispatching on the contract signal's instrument type.
func compareFamily(t *testing.T, signal *contract.Signal, native, emitted *dto.MetricFamily) {
	t.Helper()
	switch signal.MetricTypeName {
	case "gauge":
		assertEqual(t, "gauge value", gaugeTotal(native), gaugeTotal(emitted))
	case "histogram":
		n, e := histogram(t, native), histogram(t, emitted)
		assertEqual(t, "histogram sample count", float64(n.GetSampleCount()), float64(e.GetSampleCount()))
		assertEqual(t, "histogram sample sum", n.GetSampleSum(), e.GetSampleSum())
		assertBuckets(t, n, e)
	default:
		t.Fatalf("unsupported contract instrument type %q for signal %q", signal.MetricTypeName, signal.Name)
	}
}

// compareLabels asserts the exporter preserved the native label sets. The
// exporter carries native labels over unchanged, so the set of per-series label
// maps must be identical between the two families.
func compareLabels(t *testing.T, native, emitted *dto.MetricFamily) {
	t.Helper()
	n, e := labelSets(native), labelSets(emitted)
	if len(n) != len(e) {
		t.Fatalf("series count mismatch: native %d, emitted %d", len(n), len(e))
	}
	for i := range n {
		if n[i] != e[i] {
			t.Errorf("label set mismatch at series %d: native %q, emitted %q", i, n[i], e[i])
		}
	}
}

// labelSets returns a sorted, stringified label set per series so two families
// can be compared regardless of series order.
func labelSets(fam *dto.MetricFamily) []string {
	sets := make([]string, 0, len(fam.GetMetric()))
	for _, m := range fam.GetMetric() {
		pairs := make([]string, 0, len(m.GetLabel()))
		for _, l := range m.GetLabel() {
			pairs = append(pairs, l.GetName()+"="+l.GetValue())
		}
		sort.Strings(pairs)
		sets = append(sets, strings.Join(pairs, ","))
	}
	sort.Strings(sets)
	return sets
}

// gaugeTotal sums the gauge value across all series in a family.
func gaugeTotal(fam *dto.MetricFamily) float64 {
	var total float64
	for _, m := range fam.GetMetric() {
		total += m.GetGauge().GetValue()
	}
	return total
}

// histogram returns the single histogram series from a family, failing if the
// family is not a single-series histogram.
func histogram(t *testing.T, fam *dto.MetricFamily) *dto.Histogram {
	t.Helper()
	metrics := fam.GetMetric()
	if len(metrics) != 1 {
		t.Fatalf("expected exactly one series for %q, got %d", fam.GetName(), len(metrics))
	}
	h := metrics[0].GetHistogram()
	if h == nil {
		t.Fatalf("%q is not a histogram", fam.GetName())
	}
	return h
}

// assertBuckets checks the two histograms have identical cumulative buckets.
func assertBuckets(t *testing.T, native, emitted *dto.Histogram) {
	t.Helper()
	nb, eb := native.GetBucket(), emitted.GetBucket()
	if len(nb) != len(eb) {
		t.Fatalf("bucket count mismatch: native %d, emitted %d", len(nb), len(eb))
	}
	for i := range nb {
		boundary := nb[i].GetUpperBound()
		if eb[i].GetUpperBound() != boundary {
			t.Errorf("bucket %d boundary mismatch: native le=%g, emitted le=%g", i, boundary, eb[i].GetUpperBound())
			continue
		}
		assertEqual(t, fmt.Sprintf("bucket le=%g cumulative count", boundary),
			float64(nb[i].GetCumulativeCount()), float64(eb[i].GetCumulativeCount()))
	}
}

func assertEqual(t *testing.T, what string, want, got float64) {
	t.Helper()
	// Tiny delta for float sums that round-trip through text exposition; integer counts compare exactly within it.
	assert.InDelta(t, want, got, 1e-9, "%s mismatch: native=%v exporter=%v", what, want, got)
}
