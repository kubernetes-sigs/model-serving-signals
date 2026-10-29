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

package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/model-serving-signals/cmd/exporter/runner"
	"sigs.k8s.io/model-serving-signals/pkg/translator"
)

// Exercise exporter main() in a subprocess: profile selection, HTTP wiring, and a failed
// backend scrape must not turn the previous load into a fresh zero sample.
func TestRTPExporterProcess(t *testing.T) {
	for _, inject := range []bool{false, true} {
		t.Run(fmt.Sprintf("injected=%t", inject), func(t *testing.T) {
			testRTPExporterProcess(t, inject)
		})
	}
}

func testRTPExporterProcess(t *testing.T, inject bool) {
	if testing.Short() {
		t.Skip("subprocess integration")
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var invalid atomic.Bool
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/worker_status" {
			t.Errorf("path=%s", r.URL.Path)
		}
		if invalid.Load() {
			_, _ = w.Write([]byte(`{"results":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"results":[{"alive":true,"dp_rank":"0","dp_size":1,"role_type":"ROLE_TYPE_PDFUSION","running_task_info":[{"phase":"TASK_PHASE_RUNNING"},{"phase":"TASK_PHASE_RECEIVED"}]}]}`))
	}))
	defer engine.Close()
	host, enginePort, err := net.SplitHostPort(strings.TrimPrefix(engine.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, metricsPort, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	args := []string{"--profile", "../../catalog/profiles/rtp-llm.yaml", "--engine-host", host, "--engine-metrics-port", enginePort, "--engine-metrics-path", "/worker_status", "--exporter-metrics-port", metricsPort}
	if inject {
		profilePath := filepath.Join(t.TempDir(), "custom.yaml")
		data, err := os.ReadFile(args[1])
		if err != nil {
			t.Fatal(err)
		}
		data = []byte(strings.Replace(string(data), "scraper: rtp-llm-worker-status", "scraper: prometheus", 1))
		if err := os.WriteFile(profilePath, data, 0600); err != nil {
			t.Fatal(err)
		}
		args[1] = profilePath
	}
	helperArgs := append([]string{"-test.run=^TestExporterHelperProcess$", "--"}, args...)
	cmd := exec.CommandContext(ctx, binary, helperArgs...)
	cmd.Env = append(os.Environ(), "GO_WANT_EXPORTER_PROCESS=1")
	if inject {
		cmd.Env = append(cmd.Env, "GO_INJECT_RTP_SCRAPER=1")
	}
	logs, err := os.CreateTemp(t.TempDir(), "exporter-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = logs.Close() }()
	cmd.Stdout = logs
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			data, _ := os.ReadFile(logs.Name())
			t.Log(string(data))
		}
	}()
	client := &http.Client{Timeout: time.Second}
	endpoint := "http://127.0.0.1:" + metricsPort + "/metrics"
	scrape := func() (string, error) {
		resp, err := client.Get(endpoint)
		if err != nil {
			return "", err
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("status %d", resp.StatusCode)
		}
		return string(body), err
	}
	deadline := time.Now().Add(45 * time.Second)
	for {
		body, err := scrape()
		if err == nil && strings.Contains(body, "gen_ai_server_request_running") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("exporter not ready: %v %s", err, body)
		}
		time.Sleep(50 * time.Millisecond)
	}
	body, err := scrape()
	if err != nil {
		t.Fatal(err)
	}
	for _, metric := range []string{"queued", "running"} {
		expected := `gen_ai_server_request_` + metric + `{dp_rank="0",role_type="ROLE_TYPE_PDFUSION"} 1`
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %s in %s", expected, body)
		}
	}
	invalid.Store(true)
	body, err = scrape()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "gen_ai_server_request_") {
		t.Fatalf("failed scrape emitted metrics: %s", body)
	}
	invalid.Store(false)
	body, err = scrape()
	if err != nil || !strings.Contains(body, "gen_ai_server_request_running") {
		t.Fatalf("did not recover: %v %s", err, body)
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	// An unsupported profile protocol fails before the exporter starts serving.
	badProfile := filepath.Join(t.TempDir(), "unsupported.yaml")
	if err := os.WriteFile(badProfile, []byte("name: unsupported\nscraper: unknown\nsignals:\n  gen_ai.server.request.running: requests_running\n"), 0600); err != nil {
		t.Fatal(err)
	}
	rejected := exec.CommandContext(ctx, binary, append(helperArgs, "--profile", badProfile)...)
	rejected.Env = append(os.Environ(), "GO_WANT_EXPORTER_PROCESS=1")
	output, err := rejected.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "unsupported scraper kind") {
		t.Fatalf("unsupported profile scraper accepted: %v %s", err, output)
	}
}

func TestExporterHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_EXPORTER_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{os.Args[0]}, os.Args[i+1:]...)
			if os.Getenv("GO_INJECT_RTP_SCRAPER") == "1" {
				factory := func(host string, port int, path string) translator.Scraper {
					scraper, err := translator.NewScraper(translator.ScraperRTPWorkerStatus, host, port, path)
					if err != nil {
						t.Fatal(err)
					}
					return scraper
				}
				if err := runner.NewRunner().WithScraperFactory(factory).Run(ctrl.SetupSignalHandler()); err != nil {
					t.Fatal(err)
				}
			} else {
				main()
			}
			return
		}
	}
	t.Fatal("missing exporter arguments")
}
