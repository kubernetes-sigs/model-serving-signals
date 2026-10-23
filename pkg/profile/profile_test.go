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

package profile

import (
	"strings"
	"testing"

	"sigs.k8s.io/model-serving-signals/pkg/contract"
)

func TestParse(t *testing.T) {
	// Every valid case maps request.duration to vllm:e2e_request_latency_seconds.
	tests := map[string]struct {
		yaml    string
		wantErr string
	}{
		"valid": {
			yaml: "name: vllm\nsignals:\n  gen_ai.server.request.duration: vllm:e2e_request_latency_seconds\n",
		},
		"missing name": {
			yaml:    "signals:\n  gen_ai.server.request.duration: x\n",
			wantErr: "name is required",
		},
		"no signals": {
			yaml:    "name: vllm\n",
			wantErr: "at least one signal",
		},
		"unknown signal": {
			yaml:    "name: vllm\nsignals:\n  foo.bar: x\n",
			wantErr: "not a contract signal",
		},
		"empty selector": {
			yaml:    "name: vllm\nsignals:\n  gen_ai.server.request.duration: \"\"\n",
			wantErr: "is empty",
		},
		"unknown field": {
			yaml:    "name: vllm\nextra: 1\nsignals:\n  gen_ai.server.request.duration: x\n",
			wantErr: "invalid profile yaml",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			p, err := Parse([]byte(tc.yaml))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				m, ok := p.Metric(contract.RequestDuration)
				if !ok || m != "vllm:e2e_request_latency_seconds" {
					t.Errorf("Metric(RequestDuration) = %q, %v", m, ok)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestProfileScraper(t *testing.T) {
	p, err := Parse([]byte("name: custom\nscraper: rtp-llm-worker-status\nsignals:\n  gen_ai.server.request.running: requests\n"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Scraper != "rtp-llm-worker-status" {
		t.Fatalf("scraper = %q", p.Scraper)
	}
}
