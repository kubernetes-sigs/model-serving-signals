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

package utils

import (
	"fmt"
	"strings"

	"sigs.k8s.io/model-serving-signals/pkg/profile"
)

// ProfilesDir is where the engine profile catalog is installed in the exporter image.
// It is a variable so tests can point it at the catalog in the repo.
var ProfilesDir = "/catalog/profiles"

// GetProfileFromInferenceEngine returns the catalog profile whose name matches inferenceEngine
// (case-insensitive, e.g. "vllm" or "SGLang"), or an error if the catalog has no such profile.
func GetProfileFromInferenceEngine(inferenceEngine string) (*profile.Profile, error) {
	profiles, err := profile.LoadDir(ProfilesDir)
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
