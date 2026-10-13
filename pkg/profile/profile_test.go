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

// Package profile defines engine profiles: declarative mappings from an engine's native metrics onto the contract.
package profile

import (
	"strings"
	"testing"
)

func TestGetProfileFromInferenceEngine(t *testing.T) {
	origDir := ProfilesDir
	ProfilesDir = "../../catalog/profiles"
	t.Cleanup(func() { ProfilesDir = origDir })

	testCases := []struct {
		name            string
		inferenceEngine string
		expectErr       bool
	}{
		{
			name:            "find vllm",
			inferenceEngine: "vllm",
			expectErr:       false,
		},
		{
			name:            "engine name is case-insensitive",
			inferenceEngine: "VLLM",
			expectErr:       false,
		},
		{
			name:            "not find random inference engine",
			inferenceEngine: "some-random-not-existing",
			expectErr:       true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			profile, err := GetProfileFromInferenceEngine(tc.inferenceEngine)
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected an error, got profile %+v", profile)
				}
				if profile != nil {
					t.Errorf("expected nil profile on error, got %+v", profile)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if profile == nil {
				t.Fatal("expected a profile, got nil")
			}
			if !strings.EqualFold(profile.Name, tc.inferenceEngine) {
				t.Errorf("expected profile name %q, got %q", tc.inferenceEngine, profile.Name)
			}
		})
	}
}
