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

package runner

import (
	"context"
	"flag"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/pflag"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"sigs.k8s.io/model-serving-signals/pkg/exporter"
	"sigs.k8s.io/model-serving-signals/pkg/profile"
	"sigs.k8s.io/model-serving-signals/pkg/translator"
	"sigs.k8s.io/model-serving-signals/version"
)

var (
	setupLog = ctrl.Log.WithName("setup")
)

// ScraperFactory creates the Scraper used to fetch the engine's native metrics from host:port/path.
type ScraperFactory func(host string, port int, path string) translator.Scraper

// NewRunner initializes a new metrics-exporter Runner and returns its pointer.
func NewRunner() *Runner {
	return &Runner{
		scraperFactory: translator.NewHTTPScraper,
	}
}

// Runner is a struct that is used to run metrics-exporter and allows replacing the metrics scraper factory.
type Runner struct {
	scraperFactory ScraperFactory
}

// WithScraperFactory replaces the default Prometheus HTTP scraper factory and returns the Runner.
func (r *Runner) WithScraperFactory(scraperFactory ScraperFactory) *Runner {
	r.scraperFactory = scraperFactory
	return r
}

// Run runs the metrics-exporter.
func (r *Runner) Run(ctx context.Context) error {
	// parse cmd-line args
	zapOpts := zap.Options{Development: true}
	zapOpts.BindFlags(flag.CommandLine)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	opts := exporter.NewOptions()
	opts.AddFlags(pflag.CommandLine)
	pflag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zapOpts)))
	setupLog.Info("metrics-exporter build", "commit-sha", version.CommitSHA, "build-ref", version.BuildRef)

	if err := opts.Complete(); err != nil {
		setupLog.Error(err, "Failed to complete options")
		return err
	}
	if err := opts.Validate(); err != nil {
		setupLog.Error(err, "Failed to validate options")
		return err
	}

	engineProfile, err := profile.Load(opts.ProfilePath)
	if err != nil {
		setupLog.Error(err, "Failed to load engine profile")
		return err
	}
	setupLog.Info("Loaded engine profile", "engine", engineProfile.Name, "signals-count", len(engineProfile.Signals))

	ctx = log.IntoContext(ctx, ctrl.Log.WithName("metrics-exporter"))

	engineScraper := r.scraperFactory(opts.EngineHost, opts.EngineMetricsPort, opts.EngineMetricsPath)
	collector, err := translator.New(ctx, engineScraper, engineProfile, opts.ScrapeTimeout,
		translator.WithLogger(ctrl.Log.WithName("translator")),
	)
	if err != nil {
		setupLog.Error(err, "Failed to build contract collector")
		return err
	}

	registry := prometheus.NewRegistry()
	if err := registry.Register(collector); err != nil {
		setupLog.Error(err, "Failed to register contract collector")
		return err
	}

	metricsExporter := &exporter.Server{
		MetricsPort: opts.ExporterMetricsPort,
		Gatherer:    registry,
	}

	setupLog.Info("starting metrics-exporter", "engineHost", opts.EngineHost, "enginePort", opts.EngineMetricsPort, "enginePath", opts.EngineMetricsPath)
	if err := metricsExporter.Start(ctx); err != nil {
		setupLog.Error(err, "failed running metrics-exporter")
		return err
	}

	setupLog.Info("metrics-exporter terminated")
	return nil
}
