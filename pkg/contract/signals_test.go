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
		signal Signal
		want   string
	}{
		"request duration gets unit suffix": {RequestDuration, "gen_ai_server_request_duration_seconds"},
		"time to first token":               {TimeToFirstToken, "gen_ai_server_time_to_first_token_seconds"},
		"dimensionless gauge has no suffix": {RequestQueued, "gen_ai_server_request_queued"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := tc.signal.PrometheusName(); got != tc.want {
				t.Errorf("PrometheusName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAllSignalsAreConsistentAndLookupable(t *testing.T) {
	for name, s := range Signals {
		if name != s.Name {
			t.Errorf("Signals[%q] holds signal named %q", name, s.Name)
		}
		if got, ok := Lookup(s.Name); !ok || got != s {
			t.Errorf("Lookup(%q) = %+v, %v; want %+v, true", s.Name, got, ok, s)
		}
		if s.Description == "" {
			t.Errorf("signal %q has no description", s.Name)
		}
		_ = s.PrometheusName() // must not panic
	}
	if IsSignal("not.a.signal") {
		t.Error("IsSignal accepted an unknown name")
	}
}
