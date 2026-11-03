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
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"sigs.k8s.io/model-serving-signals/pkg/profile"
)

func TestTokenSpeedCollect(t *testing.T) {
	p, err := profile.New("../../catalog/profiles/tokenspeed.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// Names, types and labels were confirmed with a live TokenSpeed
	// 0.1.1.post20261102 Qwen3-0.6B scrape. Values exercise busy and idle ranks.
	const native = `# TYPE tokenspeed:num_requests_waiting gauge
tokenspeed:num_requests_waiting{model_name="tiny",app_key="",dp_rank="0"} %d
tokenspeed:num_requests_waiting{model_name="tiny",app_key="",dp_rank="1"} 0
# TYPE tokenspeed:num_requests_running gauge
tokenspeed:num_requests_running{model_name="tiny",app_key="",dp_rank="0"} %d
tokenspeed:num_requests_running{model_name="tiny",app_key="",dp_rank="1"} 0
# TYPE tokenspeed:request_queue_time_seconds histogram
tokenspeed:request_queue_time_seconds_bucket{model_name="tiny",app_key="",dp_rank="0",le="0.3"} 1
tokenspeed:request_queue_time_seconds_bucket{model_name="tiny",app_key="",dp_rank="0",le="+Inf"} 1
tokenspeed:request_queue_time_seconds_count{model_name="tiny",app_key="",dp_rank="0"} 1
tokenspeed:request_queue_time_seconds_sum{model_name="tiny",app_key="",dp_rank="0"} 0.1
`
	const want = `# HELP gen_ai_server_request_queued [ALPHA] Number of requests admitted by the server but not yet scheduled into a batch.
# TYPE gen_ai_server_request_queued gauge
gen_ai_server_request_queued{app_key="",dp_rank="0",model_name="tiny"} %d
gen_ai_server_request_queued{app_key="",dp_rank="1",model_name="tiny"} 0
`
	for _, tc := range []struct {
		name            string
		queued, running int
	}{
		{name: "busy", queued: 3, running: 5},
		{name: "idle"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/metrics" {
					t.Errorf("unexpected scrape path %q", r.URL.Path)
				}
				w.Header().Set("Content-Type", "text/plain; version=0.0.4")
				_, _ = fmt.Fprintf(w, native, tc.queued, tc.running)
			}))
			defer srv.Close()
			scraper := &HTTPScraper{url: srv.URL + "/metrics"}
			collector, err := New(context.Background(), scraper, p, testScrapeTimeout)
			if err != nil {
				t.Fatal(err)
			}
			// Compare every family: the admitted-request count must not become
			// request.running, nor queue-wait latency a client-latency histogram.
			if err := testutil.CollectAndCompare(collector, strings.NewReader(fmt.Sprintf(want, tc.queued))); err != nil {
				t.Fatal(err)
			}
		})
	}
}
