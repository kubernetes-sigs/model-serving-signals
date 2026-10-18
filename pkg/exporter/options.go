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
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/pflag"
)

const (
	DefaultExporterMetricsPort = 9464        // arbitrary port
	DefaultEngineHost          = "localhost" // the exporter runs as a sidecar of the engine
	DefaultEngineMetricsPort   = 8000
	DefaultEngineMetricsPath   = "/metrics"
	DefaultScrapeTimeout       = 5 * time.Second
)

// NewOptions returns a new Options struct initialized with the default values.
func NewOptions() *Options {
	return &Options{
		ExporterMetricsPort: DefaultExporterMetricsPort,
		EngineHost:          DefaultEngineHost,
		EngineMetricsPort:   DefaultEngineMetricsPort,
		EngineMetricsPath:   DefaultEngineMetricsPath,
		ScrapeTimeout:       DefaultScrapeTimeout,
	}
}

// Options contains configuration values necessary to create and run the metrics exporter.
type Options struct {
	ExporterMetricsPort int // Port on which the exporter serves the contract metrics endpoint.

	ProfilePath string // Path to the engine profile file.

	EngineHost        string        // Host the engine's metrics endpoint listens on.
	EngineMetricsPort int           // Port the engine's metrics endpoint listens on.
	EngineMetricsPath string        // HTTP path of the engine's metrics endpoint.
	ScrapeTimeout     time.Duration // Upper bound for a single scrape of the engine.
}

func (opts *Options) AddFlags(fs *pflag.FlagSet) {
	if fs == nil {
		fs = pflag.CommandLine
	}

	fs.IntVar(&opts.ExporterMetricsPort, "exporter-metrics-port", opts.ExporterMetricsPort, "Port on which the exporter serves the contract metrics endpoint.")
	fs.StringVar(&opts.ProfilePath, "profile", opts.ProfilePath, "Path to the engine profile YAML (required).")
	fs.StringVar(&opts.EngineHost, "engine-host", opts.EngineHost, "Host of the engine's metrics endpoint.")
	fs.IntVar(&opts.EngineMetricsPort, "engine-metrics-port", opts.EngineMetricsPort, "Port of the engine's metrics endpoint.")
	fs.StringVar(&opts.EngineMetricsPath, "engine-metrics-path", opts.EngineMetricsPath, "HTTP path of the engine's metrics endpoint.")
	fs.DurationVar(&opts.ScrapeTimeout, "scrape-timeout", opts.ScrapeTimeout, "Timeout for a single scrape of the engine.")

}

func (opts *Options) Complete() error {
	return nil
}

func (opts *Options) Validate() error {
	if opts.ExporterMetricsPort < 1 || opts.ExporterMetricsPort > 65535 {
		return fmt.Errorf("invalid --exporter-metrics-port %d: must be in range 1-65535", opts.ExporterMetricsPort)
	}
	if opts.EngineMetricsPort < 1 || opts.EngineMetricsPort > 65535 {
		return fmt.Errorf("invalid --engine-metrics-port %d: must be in range 1-65535", opts.EngineMetricsPort)
	}
	if opts.EngineHost == "" {
		return errors.New("--engine-host must not be empty")
	}
	if !strings.HasPrefix(opts.EngineMetricsPath, "/") {
		return fmt.Errorf("invalid --engine-metrics-path %q: must start with '/'", opts.EngineMetricsPath)
	}
	if opts.ScrapeTimeout <= 0 {
		return fmt.Errorf("invalid --scrape-timeout %s: must be positive", opts.ScrapeTimeout)
	}
	if opts.ProfilePath == "" {
		return errors.New("--profile is required")
	}
	return nil
}
