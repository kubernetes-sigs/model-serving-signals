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
	"errors"
	"fmt"
	"os"

	"sigs.k8s.io/yaml"

	"sigs.k8s.io/model-serving-signals/pkg/contract"
)

// Profile maps an engine's native metrics onto contract signals.
// A contract signal that is absent from Signals is one the engine does not provide.
type Profile struct {
	// Name identifies the engine, e.g. "vllm".
	Name string `json:"name"`
	// Scraper selects the source protocol. Empty means prometheus for existing profiles.
	Scraper string `json:"scraper,omitempty"`
	// Signals is keyed by contract signal name; each value is the name of the engine's native metric that
	// provides it. The native metric must have the same instrument type as the contract signal.
	Signals map[string]string `json:"signals"`
}

// Metric returns the engine's native metric that provides the given contract signal, and whether the
// engine provides it at all.
func (p *Profile) Metric(signal *contract.Signal) (string, bool) {
	m, ok := p.Signals[signal.Name]
	return m, ok
}

// Load reads and parses the profile at path.
func Load(path string) (*Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read profile from path '%s': %w", path, err)
	}
	profile, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("profile '%s': %w", path, err)
	}
	return profile, nil
}

// Parse parses and validates a profile from its YAML representation.
func Parse(data []byte) (*Profile, error) {
	profile := &Profile{}
	if err := yaml.UnmarshalStrict(data, profile); err != nil {
		return nil, fmt.Errorf("invalid profile yaml: %w", err)
	}
	if err := validate(profile); err != nil {
		return nil, fmt.Errorf("invalid profile: %w", err)
	}
	return profile, nil
}

// validate checks that the profile is well formed and maps only contract signals.
func validate(profile *Profile) error {
	var errs []error
	if profile.Name == "" {
		errs = append(errs, errors.New("name is required"))
	}
	if len(profile.Signals) == 0 {
		errs = append(errs, errors.New("signals map must contain at least one signal"))
	}
	for signal, metric := range profile.Signals {
		if !contract.IsSignal(signal) {
			errs = append(errs, fmt.Errorf("'%s' is not a contract signal", signal))
		}
		if metric == "" {
			errs = append(errs, fmt.Errorf("native metric for signal '%s' is empty", signal))
		}
	}
	return errors.Join(errs...)
}
