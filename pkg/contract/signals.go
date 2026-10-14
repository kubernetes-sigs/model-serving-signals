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

// Package contract defines the model-serving signal contract.
// It is the single place where contract signal names are defined; all other packages import them from here.
package contract

import (
	"go.opentelemetry.io/otel/semconv/v1.41.0/genaiconv"
	"k8s.io/apimachinery/pkg/util/sets"
)

// Signals adopted verbatim from the OpenTelemetry GenAI semantic conventions.
// semconv v1.41.0 is the last opentelemetry-go release that ships GenAI conventions; they have since moved to
// https://github.com/open-telemetry/semantic-conventions-genai, which has no Go package yet.
var (
	RequestDuration    = genaiconv.ServerRequestDuration{}.Name()
	TimeToFirstToken   = genaiconv.ServerTimeToFirstToken{}.Name()
	TimePerOutputToken = genaiconv.ServerTimePerOutputToken{}.Name()
)

// Signals defined by this project following the OpenTelemetry GenAI naming conventions,
// until OpenTelemetry standardizes equivalents.
const (
	// RequestQueued is the number of requests admitted by the server but not yet scheduled into a batch.
	RequestQueued = "gen_ai.server.request.queued"
	// RequestRunning is the number of requests executing forward passes.
	RequestRunning = "gen_ai.server.request.running"
)

var signals = sets.New(
	RequestDuration,
	TimeToFirstToken,
	TimePerOutputToken,
	RequestQueued,
	RequestRunning,
)

// IsSignal reports whether name is a contract signal.
func IsSignal(name string) bool {
	return signals.Has(name)
}
