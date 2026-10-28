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

package rtpllm_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"sigs.k8s.io/model-serving-signals/pkg/profile"
	"sigs.k8s.io/model-serving-signals/pkg/translator"
	"sigs.k8s.io/model-serving-signals/pkg/translator/rtpllm"
)

// Source-shaped fixture, not a live capture: frontend GrpcClientWrapper's
// MessageToDict uses enum names and strings for protobuf int64 fields.
const rtpWorkerStatusFixture = `{
  "alive": true, "dp_rank": "0", "dp_size": 2,
  "running_task_info": [{"phase":"TASK_PHASE_RUNNING"}],
  "results": [
    {"alive":true, "dp_rank":"0", "dp_size":2, "role_type":"ROLE_TYPE_PDFUSION",
     "waiting_query_len":0, "running_query_len":0,
     "running_task_info":[
       {"request_id":"1", "phase":"TASK_PHASE_RUNNING"},
       {"request_id":"2", "phase":"TASK_PHASE_RECEIVED"},
       {"request_id":"3", "phase":"TASK_PHASE_KV_ALLOCATED"},
       {"request_id":"4", "phase":"TASK_PHASE_PENDING",
        "priority_preemption_progress":"PRIORITY_PREEMPTION_CANCELING"}
     ],
     "finished_task_list":[{"phase":"TASK_PHASE_RUNNING"}]
    },
    {"alive":true, "dp_rank":"1", "dp_size":2, "role_type":"ROLE_TYPE_PDFUSION",
     "running_task_info":[]}
  ]
}`

func TestRTPWorkerStatusCollect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/worker_status" || r.Header.Get("Accept") != "application/json" {
			t.Errorf("unexpected request: path=%q Accept=%q", r.URL.Path, r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(rtpWorkerStatusFixture))
	}))
	defer srv.Close()
	p, err := profile.Load("../../../catalog/profiles/rtp-llm.yaml")
	if err != nil {
		t.Fatal(err)
	}
	scraper := &rtpllm.Scraper{URL: srv.URL + "/worker_status"}
	collector, err := translator.New(context.Background(), scraper, p, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	want := `# HELP gen_ai_server_request_queued Number of requests admitted by the server but not yet scheduled into a batch.
# TYPE gen_ai_server_request_queued gauge
gen_ai_server_request_queued{dp_rank="0",role_type="ROLE_TYPE_PDFUSION"} 2
gen_ai_server_request_queued{dp_rank="1",role_type="ROLE_TYPE_PDFUSION"} 0
# HELP gen_ai_server_request_running Number of requests executing forward passes.
# TYPE gen_ai_server_request_running gauge
gen_ai_server_request_running{dp_rank="0",role_type="ROLE_TYPE_PDFUSION"} 1
gen_ai_server_request_running{dp_rank="1",role_type="ROLE_TYPE_PDFUSION"} 0
`
	if err := testutil.CollectAndCompare(collector, strings.NewReader(want)); err != nil {
		t.Fatal(err)
	}
}

func TestRTPWorkerStatusRejectsInvalidSnapshots(t *testing.T) {
	tests := map[string]string{
		"invalid JSON":       `{`,
		"trailing JSON":      rtpWorkerStatusFixture + `{}`,
		"backend error":      `{"error":"No backend available for worker_status"}`,
		"health endpoint":    `{"alive":true,"available_kv_cache":100}`,
		"empty results":      `{"results":[]}`,
		"null results":       `{"results":null}`,
		"dead worker":        strings.Replace(rtpWorkerStatusFixture, `"alive":true`, `"alive":false`, 1),
		"partial DP results": strings.ReplaceAll(rtpWorkerStatusFixture, `"dp_size":2`, `"dp_size":3`),
		"duplicate rank":     strings.Replace(rtpWorkerStatusFixture, `"dp_rank":"1"`, `"dp_rank":"0"`, 1),
		"missing rank":       strings.Replace(rtpWorkerStatusFixture, `"dp_rank":"1",`, ``, 1),
		"negative rank":      strings.Replace(rtpWorkerStatusFixture, `"dp_rank":"1"`, `"dp_rank":"-1"`, 1),
		"fractional rank":    strings.Replace(rtpWorkerStatusFixture, `"dp_rank":"1"`, `"dp_rank":0.5`, 1),
		"missing tasks":      strings.Replace(rtpWorkerStatusFixture, `"running_task_info":[]`, `"other":[]`, 1),
		"null tasks":         strings.Replace(rtpWorkerStatusFixture, `"running_task_info":[]`, `"running_task_info":null`, 1),
		"unknown phase":      strings.ReplaceAll(rtpWorkerStatusFixture, `TASK_PHASE_RUNNING`, `TASK_PHASE_NEW`),
		"missing phase":      strings.ReplaceAll(rtpWorkerStatusFixture, `"phase":`, `"old_phase":`),
		"wrong role":         strings.ReplaceAll(rtpWorkerStatusFixture, `ROLE_TYPE_PDFUSION`, `ROLE_TYPE_VIT`),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()
			families, err := (&rtpllm.Scraper{URL: srv.URL}).Scrape(context.Background())
			if err == nil || families != nil {
				t.Fatalf("got families=%v, err=%v; want no metrics and an error", families, err)
			}
		})
	}
}

func TestRTPWorkerStatusTransportFailures(t *testing.T) {
	t.Run("HTTP error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer srv.Close()
		scraper := &rtpllm.Scraper{URL: srv.URL}
		if _, err := scraper.Scrape(context.Background()); err == nil {
			t.Fatal("expected HTTP error")
		}
	})
	t.Run("deadline", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		defer srv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		scraper := &rtpllm.Scraper{URL: srv.URL}
		if _, err := scraper.Scrape(ctx); err == nil {
			t.Fatal("expected deadline error")
		}
	})
}
