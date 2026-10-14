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

	ctx := ctrl.SetupSignalHandler()
	ctx = log.IntoContext(ctx, ctrl.Log.WithName("metrics-exporter"))

	metricsExporter := &exporter.Server{
		MetricsPort:     opts.MetricsPort,
		Gatherer:        prometheus.NewRegistry(),
		InferenceEngine: opts.InferenceEngine,
	}

	setupLog.Info("Starting metrics exporter")
	if err := metricsExporter.Start(ctx); err != nil {
		setupLog.Error(err, "failed running metrics exporter")
		os.Exit(1)
	}
}
