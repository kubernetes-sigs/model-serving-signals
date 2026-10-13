/*
Copyright 2026 The Kubernetes Authors.

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
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"sigs.k8s.io/model-serving-signals/pkg/profile"
)

const (
	shutdownTimeout   = 5 * time.Second
	readHeaderTimeout = 10 * time.Second
)

// Server serves the contract metrics endpoint.
type Server struct {
	MetricsPort     int
	Gatherer        prometheus.Gatherer
	InferenceEngine string
}

// Start serves the metrics endpoint until ctx is cancelled, then shuts the server down gracefully.
func (s *Server) Start(ctx context.Context) error {
	logger := log.FromContext(ctx)

	// var engineProfile *profile.Profile
	engineProfile, err := profile.GetProfileFromInferenceEngine(s.InferenceEngine)
	if err != nil {
		return fmt.Errorf("failed to get profile for inference engine '%s': %w", s.InferenceEngine, err)
	}
	logger.Info("Serving metrics for inference engine", "profile", engineProfile.Name)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(s.Gatherer, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	address := fmt.Sprintf(":%d", s.MetricsPort)
	server := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	go func() {
		<-ctx.Done()
		logger.Info("Shutting down metrics exporter")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error(err, "Failed to shut down metrics exporter gracefully")
		}
	}()

	logger.Info("Serving metrics", "port", s.MetricsPort)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("metrics server failed: %w", err)
	}
	return nil
}
