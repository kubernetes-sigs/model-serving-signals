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

package exporter

import (
	"fmt"

	"github.com/spf13/pflag"
)

const (
	DefaultMetricsPort     = 9464 // arbitrary port
	DefaultInferenceEngine = "vllm"
)

// NewOptions returns a new Options struct initialized with the default values.
func NewOptions() *Options {
	return &Options{
		MetricsPort:     DefaultMetricsPort,
		InferenceEngine: DefaultInferenceEngine,
	}
}

// Options contains configuration values necessary to create and run the metrics exporter.
type Options struct {
	MetricsPort     int    // Port on which the exporter serves the contract metrics endpoint.
	InferenceEngine string // inference engine of the exported next to the inference engine.
}

func (opts *Options) AddFlags(fs *pflag.FlagSet) {
	if fs == nil {
		fs = pflag.CommandLine
	}

	fs.IntVar(&opts.MetricsPort, "metrics-port", opts.MetricsPort, "Port on which the exporter serves the contract metrics endpoint.")
	fs.StringVar(&opts.InferenceEngine, "inference-engine", opts.InferenceEngine, "Inference engine running next to the exporter, whose profile is used to export the contract metrics.")
}

func (opts *Options) Complete() error {
	return nil
}

func (opts *Options) Validate() error {
	if opts.MetricsPort < 1 || opts.MetricsPort > 65535 {
		return fmt.Errorf("invalid --metrics-port %d: must be in range 1-65535", opts.MetricsPort)
	}
	return nil
}
