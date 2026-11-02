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

import (
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"sigs.k8s.io/yaml"
)

var update = flag.Bool("update", false, "update the golden contract signals file")

// goldenSignal is the reviewable snapshot of a contract signal. Its stability level is carried in the
// Description prefix (e.g. "[ALPHA] ..."), so bumping a signal's stability shows up as a diff here too.
type goldenSignal struct {
	Name           string `json:"name"`
	Unit           string `json:"unit"`
	Type           string `json:"type"`
	PrometheusName string `json:"prometheusName"`
	Description    string `json:"description"`
}

// TestSignalsGolden locks the contract: any change to a signal's name, unit, type, Prometheus name,
// description, or stability must be an explicit, reviewed change to the golden file. Regenerate it with
// `make update-contract-golden` and review the resulting diff.
func TestSignalsGolden(t *testing.T) {
	golden := filepath.Join("testdata", "signals.yaml")

	snapshot := make([]goldenSignal, 0, len(Signals))
	for _, s := range Signals {
		snapshot = append(snapshot, goldenSignal{
			Name:           s.Name,
			Unit:           s.Unit,
			Type:           s.MetricTypeName,
			PrometheusName: s.PrometheusName,
			Description:    s.Description,
		})
	}
	sort.Slice(snapshot, func(i, j int) bool { return snapshot[i].Name < snapshot[j].Name })

	got, err := yaml.Marshal(snapshot)
	if err != nil {
		t.Fatalf("failed to marshal contract signals: %v", err)
	}

	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatalf("failed to update golden file: %v", err)
		}
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("failed to read golden file (create it with `make update-contract-golden`): %v", err)
	}

	if string(got) != string(want) {
		t.Errorf("contract signals differ from the golden file; if this change is intended, regenerate it with `make update-contract-golden` and review the diff.\ngot:\n%s\nwant:\n%s", got, want)
	}
}
