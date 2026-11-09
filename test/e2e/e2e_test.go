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

// Package e2e verifies the metrics exporter end to end against simulated
// inference engines. hack/test-e2e.sh creates a kind cluster and loads the
// images; this test does the rest. For each engine in E2E_ENGINES it deploys the
// Dynamo mocker + exporter pod, port-forwards it, drives inference load, and
// checks every contract signal against the native metric that feeds it.
//
// The test controls timing: it first lets a batch of requests complete
// (populating histograms), then holds one request in-flight so the running
// gauge is non-zero while both endpoints are scraped.
//
// The test is engine-agnostic: the engine profile is the only source of truth
// for which native metric maps to which contract signal, so adding an engine
// means adding a profile, not changing this code.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"

	"sigs.k8s.io/model-serving-signals/pkg/contract"
	"sigs.k8s.io/model-serving-signals/pkg/profile"
	"sigs.k8s.io/model-serving-signals/pkg/translator"
)

const (
	// go test runs in the package directory.
	profilesDir    = "../../catalog/profiles"
	defaultEngines = "vllm sglang trt-llm"
	forwardHost    = "127.0.0.1"
	metricsPath    = "/metrics"
	warmupCount    = 5
	scrapeTimeout  = 5 * time.Second
)

// endpoints are the forwarded local addresses of one engine's pod.
type endpoints struct {
	inference, engineMetrics, exporter int
}

func (e endpoints) inferenceURL(path string) string {
	return fmt.Sprintf("http://%s:%d%s", forwardHost, e.inference, path)
}

func (e endpoints) engineMetricsURL() string {
	return fmt.Sprintf("http://%s:%d%s", forwardHost, e.engineMetrics, metricsPath)
}

func (e endpoints) exporterMetricsURL() string {
	return fmt.Sprintf("http://%s:%d%s", forwardHost, e.exporter, metricsPath)
}

// TestExporterMatchesEngine runs the check once per engine. Engines run one at
// a time because they share the pod name and the cluster.
func TestExporterMatchesEngine(t *testing.T) {
	c := newCluster(t)
	engines := os.Getenv("E2E_ENGINES")
	if engines == "" {
		engines = defaultEngines
	}
	for _, engine := range strings.Fields(engines) {
		t.Run(engine, func(t *testing.T) {
			prof, err := profile.New(filepath.Join(profilesDir, engine+".yaml"))
			if err != nil {
				t.Fatalf("load profile: %v", err)
			}
			pod := renderPod(t, engine)
			c.deploy(t, pod)

			ports := []int32{
				containerPort(t, pod, inferencePortName),
				containerPort(t, pod, engineMetricsPortName),
				containerPort(t, pod, exporterPortName),
			}
			local := c.forward(t, pod.Name, ports...)
			ep := endpoints{inference: local[ports[0]], engineMetrics: local[ports[1]], exporter: local[ports[2]]}

			model := servedModel(t, pod)
			waitServable(t, ep, model)
			checkExporter(t, prof, ep, model)
		})
	}
}

// checkExporter proves the full scrape -> map -> emit path: for every signal
// the profile declares, the exporter's contract metric must exactly match the
// engine's native metric that feeds it, including its label set.
func checkExporter(t *testing.T, prof *profile.Profile, ep endpoints, model string) {
	t.Helper()
	if len(prof.Signals) == 0 {
		t.Fatal("profile declares no signals")
	}
	engineScraper := translator.NewHTTPScraper(forwardHost, ep.engineMetrics, metricsPath)
	exporterScraper := translator.NewHTTPScraper(forwardHost, ep.exporter, metricsPath)
	completionsURL := ep.inferenceURL("/v1/completions")

	// Phase 1: let a batch of requests complete so histograms are populated.
	for i := 0; i < warmupCount; i++ {
		if err := infer(t.Context(), completionsURL, model, 16); err != nil {
			t.Fatalf("warmup inference request: %v", err)
		}
	}

	// Phase 2: hold one request in-flight so the running gauge is > 0, then
	// scrape both endpoints as close together as possible.
	var wg sync.WaitGroup
	wg.Add(1)
	inflightCtx, cancelInflight := context.WithCancel(t.Context())
	defer func() { cancelInflight(); wg.Wait() }()
	go func() {
		defer wg.Done()
		// The mocker's slowed decode keeps this request running long enough to observe.
		_ = infer(inflightCtx, completionsURL, model, 256)
	}()

	const runningSignal = "gen_ai.server.request.running"
	runningNative, ok := prof.Signals[runningSignal]
	if !ok {
		t.Fatalf("profile does not map %q; cannot gate on in-flight requests", runningSignal)
	}
	waitForRunning(t, engineScraper, runningNative, scrapeTimeout)

	ctx, cancel := context.WithTimeout(t.Context(), scrapeTimeout)
	defer cancel()
	native, err := engineScraper.Scrape(ctx)
	if err != nil {
		t.Fatalf("scrape engine: %v", err)
	}
	emitted, err := exporterScraper.Scrape(ctx)
	if err != nil {
		t.Fatalf("scrape exporter: %v", err)
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
			compareFamilies(t, signal, nativeFam, contractFam)
		})
	}
}

// waitForRunning polls the engine until the given native gauge is > 0, so the
// in-flight request is actually observable before the test scrapes.
func waitForRunning(t *testing.T, scraper translator.Scraper, nativeName string, timeout time.Duration) {
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

// infer sends a single completion request to the frontend.
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

// compareFamilies asserts the emitted contract family carries exactly the
// native family's series, matched by label set, with identical values. The
// exporter carries native labels over unchanged, so every native series must
// have a counterpart. Engines may split one metric into several series (for
// example SGLang's is_streaming label), so series are compared one by one
// rather than assuming a single series per family.
func compareFamilies(t *testing.T, signal *contract.Signal, native, emitted *dto.MetricFamily) {
	t.Helper()
	n, e := seriesByLabels(native), seriesByLabels(emitted)
	if !assert.ElementsMatch(t, sortedKeys(n), sortedKeys(e), "label sets differ between native and exporter") {
		return
	}
	for key, nm := range n {
		compareSeries(t, signal, key, nm, e[key])
	}
}

// compareSeries asserts one emitted series equals its native counterpart,
// dispatching on the contract signal's instrument type.
func compareSeries(t *testing.T, signal *contract.Signal, labels string, native, emitted *dto.Metric) {
	t.Helper()
	switch signal.MetricTypeName {
	case "gauge":
		assertEqual(t, fmt.Sprintf("{%s} gauge value", labels), native.GetGauge().GetValue(), emitted.GetGauge().GetValue())
	case "histogram":
		n, e := native.GetHistogram(), emitted.GetHistogram()
		if n == nil || e == nil {
			t.Fatalf("{%s} is not a histogram on both sides", labels)
		}
		assertEqual(t, fmt.Sprintf("{%s} histogram sample count", labels), float64(n.GetSampleCount()), float64(e.GetSampleCount()))
		assertEqual(t, fmt.Sprintf("{%s} histogram sample sum", labels), n.GetSampleSum(), e.GetSampleSum())
		assertBuckets(t, labels, n, e)
	default:
		t.Fatalf("unsupported contract instrument type %q for signal %q", signal.MetricTypeName, signal.Name)
	}
}

// seriesByLabels indexes a family's series by a sorted, stringified label set
// so two families can be compared regardless of series order.
func seriesByLabels(fam *dto.MetricFamily) map[string]*dto.Metric {
	series := make(map[string]*dto.Metric, len(fam.GetMetric()))
	for _, m := range fam.GetMetric() {
		pairs := make([]string, 0, len(m.GetLabel()))
		for _, l := range m.GetLabel() {
			pairs = append(pairs, l.GetName()+"="+l.GetValue())
		}
		sort.Strings(pairs)
		series[strings.Join(pairs, ",")] = m
	}
	return series
}

func sortedKeys(m map[string]*dto.Metric) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// gaugeTotal sums the gauge value across all series in a family.
func gaugeTotal(fam *dto.MetricFamily) float64 {
	var total float64
	for _, m := range fam.GetMetric() {
		total += m.GetGauge().GetValue()
	}
	return total
}

// assertBuckets checks the two histograms have identical cumulative buckets.
func assertBuckets(t *testing.T, labels string, native, emitted *dto.Histogram) {
	t.Helper()
	nb, eb := native.GetBucket(), emitted.GetBucket()
	if len(nb) != len(eb) {
		t.Fatalf("{%s} bucket count mismatch: native %d, emitted %d", labels, len(nb), len(eb))
	}
	for i := range nb {
		boundary := nb[i].GetUpperBound()
		if eb[i].GetUpperBound() != boundary {
			t.Errorf("{%s} bucket %d boundary mismatch: native le=%g, emitted le=%g", labels, i, boundary, eb[i].GetUpperBound())
			continue
		}
		assertEqual(t, fmt.Sprintf("{%s} bucket le=%g cumulative count", labels, boundary),
			float64(nb[i].GetCumulativeCount()), float64(eb[i].GetCumulativeCount()))
	}
}

func assertEqual(t *testing.T, what string, want, got float64) {
	t.Helper()
	// Tiny delta for float sums that round-trip through text exposition; integer counts compare exactly within it.
	assert.InDelta(t, want, got, 1e-9, "%s mismatch: native=%v exporter=%v", what, want, got)
}
