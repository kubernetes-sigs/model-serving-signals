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

package e2e

import (
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Images default to these tags; hack/test-e2e.sh reads them from here to load
// them into kind. E2E_DYNAMO_IMAGE and E2E_EXPORTER_IMAGE override them.
const (
	// A nightly, because no stable Dynamo release includes ai-dynamo/dynamo#17200 yet.
	defaultDynamoImage   = "nvcr.io/nvidia/ai-dynamo/dynamo-planner-nightly:20261103-e37f0b9"
	defaultExporterImage = "model-serving-signals/exporter:dev"
)

const (
	podName = "mss-e2e"

	inferencePort     = 8000
	engineMetricsPort = 8081
	exporterPort      = 9464

	// mockerModel is the model the mocker serves; only its tokenizer is downloaded.
	mockerModel = "Qwen/Qwen3-0.6B"

	// The frontend and mocker find each other through file-based discovery on
	// this shared volume, so no Dynamo operator, etcd or NATS is needed.
	discoveryVolume = "dynamo-kv"
	discoveryPath   = "/dynamo-kv"
)

// mockerEngineTypes maps a catalog profile name to the mocker's --engine-type.
var mockerEngineTypes = map[string]string{
	"vllm":    "vllm",
	"sglang":  "sglang",
	"trt-llm": "trtllm",
}

// mockerPod returns the e2e workload for one engine: a CPU-only Dynamo
// frontend and mocker plus the exporter, all in one pod. The mocker emits the
// engine-native metrics selected by --engine-type; the exporter scrapes them
// over localhost and re-emits the mapped contract metrics.
func mockerPod(t *testing.T, engine string) *corev1.Pod {
	t.Helper()
	engineType, ok := mockerEngineTypes[engine]
	if !ok {
		t.Fatalf("no mocker engine type for profile %q", engine)
	}
	dynamoImage := envOr("E2E_DYNAMO_IMAGE", defaultDynamoImage)
	discovery := corev1.EnvVar{Name: "DYN_FILE_KV", Value: discoveryPath}
	mounts := []corev1.VolumeMount{{Name: discoveryVolume, MountPath: discoveryPath}}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   podName,
			Labels: map[string]string{"app": podName},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Volumes: []corev1.Volume{{
				Name:         discoveryVolume,
				VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
			}},
			Containers: []corev1.Container{
				{
					// OpenAI-compatible frontend that routes inference to the mocker.
					Name:  "frontend",
					Image: dynamoImage,
					Command: []string{
						"python3", "-m", "dynamo.frontend",
						"--http-port=8000",
						"--discovery-backend=file",
					},
					Env:            []corev1.EnvVar{discovery},
					VolumeMounts:   mounts,
					Ports:          []corev1.ContainerPort{{Name: "inference", ContainerPort: inferencePort}},
					ReadinessProbe: httpProbe("/health", inferencePort, 5),
				},
				{
					// CPU-only simulated engine: fakes inference and emits
					// engine-native metrics from its system server.
					Name:  "engine",
					Image: dynamoImage,
					Command: []string{
						"python3", "-m", "dynamo.mocker",
						"--model-path=" + mockerModel,
						"--engine-type=" + engineType,
						"--discovery-backend=file",
						// Slow simulated prefill and decode so a request stays in-flight
						// long enough for the test to observe a non-zero running gauge.
						"--speedup-ratio=0.1",
					},
					Env:            []corev1.EnvVar{discovery, {Name: "DYN_SYSTEM_PORT", Value: "8081"}},
					VolumeMounts:   mounts,
					Ports:          []corev1.ContainerPort{{Name: "engine-metrics", ContainerPort: engineMetricsPort}},
					ReadinessProbe: httpProbe(metricsPath, engineMetricsPort, 5),
				},
				{
					// Built by `make image-load` and loaded into the kind cluster.
					Name:            "exporter",
					Image:           envOr("E2E_EXPORTER_IMAGE", defaultExporterImage),
					ImagePullPolicy: corev1.PullNever,
					Args: []string{
						"--profile=/catalog/profiles/" + engine + ".yaml",
						"--engine-host=localhost",
						"--engine-metrics-port=8081",
						"--exporter-metrics-port=9464",
					},
					Ports:          []corev1.ContainerPort{{Name: "contract", ContainerPort: exporterPort}},
					ReadinessProbe: httpProbe(metricsPath, exporterPort, 2),
				},
			},
		},
	}
}

func httpProbe(path string, port int32, initialDelaySeconds int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{Path: path, Port: intstr.FromInt32(port)},
		},
		InitialDelaySeconds: initialDelaySeconds,
		PeriodSeconds:       2,
	}
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
