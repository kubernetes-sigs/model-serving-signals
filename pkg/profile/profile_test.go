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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sigs.k8s.io/model-serving-signals/pkg/contract"
)

const (
	// requestDurationSignal is the contract signal the test profiles map.
	requestDurationSignal = "gen_ai.server.request.duration"
	// requestDurationMetric is the test metric that provides requestDurationSignal.
	requestDurationMetric = "test:e2e_request_latency_seconds"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name            string
		yaml            string
		path            string // when set, New reads this path instead of a file written from yaml
		expectedProfile *Profile
		expectedErr     string // substring of the expected error; empty when New should succeed
	}{
		{
			name: "Valid profile",
			yaml: `
name: test
signals:
  gen_ai.server.request.duration: test:e2e_request_latency_seconds
  gen_ai.server.request.running: test:num_requests_running
`,
			expectedProfile: &Profile{
				Name: "test",
				Signals: map[string]string{
					requestDurationSignal:           requestDurationMetric,
					"gen_ai.server.request.running": "test:num_requests_running",
				},
			},
		},
		{
			name: "Missing name",
			yaml: `
signals:
  gen_ai.server.request.duration: test:e2e_request_latency_seconds
`,
			expectedErr: "name is required",
		},
		{
			name: "No signals",
			yaml: `
name: test
`,
			expectedErr: "at least one signal",
		},
		{
			name: "Unknown signal",
			yaml: `
name: test
signals:
  foo.bar: test:e2e_request_latency_seconds
`,
			expectedErr: "not a contract signal",
		},
		{
			name: "Empty native metric",
			yaml: `
name: test
signals:
  gen_ai.server.request.duration: ""
`,
			expectedErr: "is empty",
		},
		{
			name: "Unknown field",
			yaml: `
name: test
extra: 1
signals:
  gen_ai.server.request.duration: test:e2e_request_latency_seconds
`,
			expectedErr: "invalid profile yaml",
		},
		{
			name:        "Missing file",
			path:        filepath.Join(t.TempDir(), "does-not-exist.yaml"),
			expectedErr: "failed to read profile",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := test.path
			if path == "" {
				path = filepath.Join(t.TempDir(), "profile.yaml")
				require.NoError(t, os.WriteFile(path, []byte(test.yaml), 0o600))
			}

			profile, err := New(path)

			if test.expectedErr != "" {
				assert.ErrorContains(t, err, test.expectedErr)
				assert.Nil(t, profile)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.expectedProfile, profile)
		})
	}
}

func TestMetric(t *testing.T) {
	profile := &Profile{
		Name: "test",
		Signals: map[string]string{
			requestDurationSignal: requestDurationMetric,
		},
	}

	tests := []struct {
		name             string
		signal           string
		expectedMetric   string
		expectedProvided bool
	}{
		{
			name:             "Signal provided by the engine",
			signal:           requestDurationSignal,
			expectedMetric:   requestDurationMetric,
			expectedProvided: true,
		},
		{
			name:             "Signal not provided by the engine",
			signal:           "gen_ai.server.request.queued", // absent from the profile
			expectedMetric:   "",
			expectedProvided: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			signal, ok := contract.Signals[test.signal]
			require.True(t, ok, "contract signal %s is not registered", test.signal)

			metric, provided := profile.Metric(signal)

			assert.Equal(t, test.expectedProvided, provided)
			assert.Equal(t, test.expectedMetric, metric)
		})
	}
}
