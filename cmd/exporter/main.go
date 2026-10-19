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

package main

import (
	"flag"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/pflag"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"sigs.k8s.io/model-serving-signals/pkg/exporter"
	"sigs.k8s.io/model-serving-signals/pkg/profile"
	"sigs.k8s.io/model-serving-signals/pkg/translator"
)

var (
	setupLog = ctrl.Log.WithName("setup")
)

func main() {
	// parse cmd-line args
	zapOpts := zap.Options{Development: true}
	zapOpts.BindFlags(flag.CommandLine)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	opts := exporter.NewOptions()
	opts.AddFlags(pflag.CommandLine)
	pflag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zapOpts)))

	if err := opts.Complete(); err != nil {
		setupLog.Error(err, "Failed to complete options")
		os.Exit(1)
	}
	if err := opts.Validate(); err != nil {
		setupLog.Error(err, "Failed to validate options")
		os.Exit(1)
	}

	engineProfile, err := profile.Load(opts.ProfilePath)
	if err != nil {
		setupLog.Error(err, "Failed to load engine profile")
		os.Exit(1)
	}
	setupLog.Info("Loaded engine profile", "engine", engineProfile.Name, "signals", len(engineProfile.Signals))

	ctx := ctrl.SetupSignalHandler()
	ctx = log.IntoContext(ctx, ctrl.Log.WithName("metrics-exporter"))

	engineScraper := translator.NewHTTPScraper(opts.EngineHost, opts.EngineMetricsPort, opts.EngineMetricsPath)
	collector, err := translator.New(ctx, engineScraper, engineProfile, opts.ScrapeTimeout,
		translator.WithLogger(ctrl.Log.WithName("translator")),
	)
	if err != nil {
		setupLog.Error(err, "Failed to build contract collector")
		os.Exit(1)
	}

	registry := prometheus.NewRegistry()
	if err := registry.Register(collector); err != nil {
		setupLog.Error(err, "Failed to register contract collector")
		os.Exit(1)
	}

	metricsExporter := &exporter.Server{
		MetricsPort: opts.ExporterMetricsPort,
		Gatherer:    registry,
	}

	setupLog.Info("Starting metrics exporter", "engineMetricsURL", engineScraper.URL)
	if err := metricsExporter.Start(ctx); err != nil {
		setupLog.Error(err, "failed running metrics exporter")
		os.Exit(1)
	}
}
