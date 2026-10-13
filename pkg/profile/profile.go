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
	"path/filepath"
	"strings"

	"sigs.k8s.io/yaml"

	"sigs.k8s.io/model-serving-signals/pkg/contract"
)

// ProfilesDir is where the engine profile catalog is installed in the exporter image.
// It is a variable so tests can point it at the catalog in the repo.
var ProfilesDir = "/catalog/profiles"

// Profile maps an engine's native metrics onto contract signals.
// A contract signal that is absent from Signals is one the engine does not provide.
type Profile struct {
	// Name identifies the engine, e.g. "vllm".
	Name string `json:"name"`
	// Signals is keyed by contract signal name; values are selectors of the engine's native metric.
	Signals map[string]string `json:"signals"`
}

// Load reads and parses the profile at path.
func Load(path string) (*Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read profile from path '%s': %w", path, err)
	}
	profile := &Profile{}
	if err := yaml.UnmarshalStrict(data, profile); err != nil {
		return nil, fmt.Errorf("invalid profile yaml - '%s': %w", path, err)
	}
	if err := validate(profile); err != nil {
		return nil, fmt.Errorf("invalid profile - '%s': %w", path, err)
	}
	return profile, nil
}

// LoadDir reads and parses every *.yaml profile in dir.
func loadDir(dir string) ([]*Profile, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, fmt.Errorf("failed to list profiles in '%s': %w", dir, err)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no profiles found in '%s'", dir)
	}
	profiles := make([]*Profile, 0, len(paths))
	seen := make(map[string]string, len(paths))
	for _, path := range paths {
		profile, err := Load(path)
		if err != nil {
			return nil, err
		}
		if other, ok := seen[profile.Name]; ok {
			return nil, fmt.Errorf("profile name '%s' is defined in both '%s' and '%s'", profile.Name, other, path)
		}
		seen[profile.Name] = path
		profiles = append(profiles, profile)
	}
	return profiles, nil
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
	for signal, selector := range profile.Signals {
		if !contract.IsSignal(signal) {
			errs = append(errs, fmt.Errorf("'%s' is not a contract signal", signal))
		}
		if selector == "" {
			errs = append(errs, fmt.Errorf("mapping selector for signal '%s' is empty", signal))
		}
	}
	return errors.Join(errs...)
}

// GetProfileFromInferenceEngine returns the catalog profile whose name matches inferenceEngine
// (case-insensitive, e.g. "vllm" or "SGLang"), or an error if the catalog has no such profile.
func GetProfileFromInferenceEngine(inferenceEngine string) (*Profile, error) {
	profiles, err := loadDir(ProfilesDir)
	if err != nil {
		return nil, err
	}
	for _, p := range profiles {
		if strings.EqualFold(p.Name, inferenceEngine) {
			return p, nil
		}
	}
	return nil, fmt.Errorf("no profile found for inference engine '%s' in '%s'", inferenceEngine, ProfilesDir)
}
