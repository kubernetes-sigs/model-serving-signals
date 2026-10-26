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

package contract

import "testing"

func TestPrometheusName(t *testing.T) {
	tests := map[string]struct {
		signal string
		want   string
	}{
		"request duration gets unit suffix": {"gen_ai.server.request.duration", "gen_ai_server_request_duration_seconds"},
		"time to first token":               {"gen_ai.server.time_to_first_token", "gen_ai_server_time_to_first_token_seconds"},
		"unit gauge has no suffix":          {"gen_ai.server.request.queued", "gen_ai_server_request_queued"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			signal, ok := Signals[tc.signal]
			if !ok {
				t.Fatalf("signal %s is not registered", tc.signal)
			}
			if got := signal.PrometheusName; got != tc.want {
				t.Errorf("PrometheusName = %s, want %s", got, tc.want)
			}
		})
	}
}
