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
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"sigs.k8s.io/model-serving-signals/pkg/translator"
)

const (
	defaultNamespace = "default"
	metricsPath      = "/metrics"

	// The first engine downloads the tokenizer, so readiness can take a while.
	podReadyTimeout  = 10 * time.Minute
	servableTimeout  = 2 * time.Minute
	podDeleteTimeout = 2 * time.Minute
	pollInterval     = 2 * time.Second
	logTailLines     = 50

	// requestTimeout bounds every HTTP request to the pod. Port-forward streams
	// do not support deadlines, so this is what stops a hung request.
	requestTimeout = 2 * time.Minute
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
					Env:          []corev1.EnvVar{discovery},
					VolumeMounts: mounts,
					Ports:        []corev1.ContainerPort{{Name: "inference", ContainerPort: inferencePort}},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromInt32(inferencePort)},
						},
						InitialDelaySeconds: 5,
						PeriodSeconds:       2,
					},
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
					Env:          []corev1.EnvVar{discovery, {Name: "DYN_SYSTEM_PORT", Value: "8081"}},
					VolumeMounts: mounts,
					Ports:        []corev1.ContainerPort{{Name: "engine-metrics", ContainerPort: engineMetricsPort}},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{Path: metricsPath, Port: intstr.FromInt32(engineMetricsPort)},
						},
						InitialDelaySeconds: 5,
						PeriodSeconds:       2,
					},
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
					Ports: []corev1.ContainerPort{{Name: "contract", ContainerPort: exporterPort}},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{Path: metricsPath, Port: intstr.FromInt32(exporterPort)},
						},
						InitialDelaySeconds: 2,
						PeriodSeconds:       2,
					},
				},
			},
		},
	}
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// cluster wraps the clients for the kind cluster created by hack/test-e2e.sh.
type cluster struct {
	config    *rest.Config
	client    kubernetes.Interface
	namespace string
}

// newCluster connects to the context in E2E_KUBE_CONTEXT, and skips the test
// when it is unset so a plain `go test ./...` never touches a real cluster.
func newCluster(t *testing.T) *cluster {
	t.Helper()
	kubeContext := os.Getenv("E2E_KUBE_CONTEXT")
	if kubeContext == "" {
		t.Skip("E2E_KUBE_CONTEXT is unset; run `make test-e2e`")
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: kubeContext},
	).ClientConfig()
	if err != nil {
		t.Fatalf("load kubeconfig context %q: %v", kubeContext, err)
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatalf("create Kubernetes client: %v", err)
	}
	return &cluster{
		config:    config,
		client:    client,
		namespace: envOr("E2E_GPU_NAMESPACE", defaultNamespace),
	}
}

// deploy creates the pod, waits until it is ready, and registers a cleanup
// that dumps its state on failure and deletes it, so the next engine can
// reuse the pod name.
func (c *cluster) deploy(t *testing.T, pod *corev1.Pod) {
	t.Helper()
	pods := c.client.CoreV1().Pods(c.namespace)
	if _, err := pods.Create(t.Context(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod %s: %v", pod.Name, err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), podDeleteTimeout)
		defer cancel()
		if t.Failed() {
			c.dumpPod(ctx, t, pod.Name)
		}
		c.deletePod(ctx, t, pod.Name)
	})

	err := wait.PollUntilContextTimeout(t.Context(), pollInterval, podReadyTimeout, true, func(ctx context.Context) (bool, error) {
		current, err := pods.Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if current.Status.Phase == corev1.PodFailed || current.Status.Phase == corev1.PodSucceeded {
			return false, fmt.Errorf("pod exited with phase %s", current.Status.Phase)
		}
		return isReady(current), nil
	})
	if err != nil {
		t.Fatalf("wait for pod %s to be ready: %v", pod.Name, err)
	}
}

func isReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}

func (c *cluster) deletePod(ctx context.Context, t *testing.T, name string) {
	pods := c.client.CoreV1().Pods(c.namespace)
	gracePeriod := int64(0)
	err := pods.Delete(ctx, name, metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod})
	if err != nil && !apierrors.IsNotFound(err) {
		t.Errorf("delete pod %s: %v", name, err)
		return
	}
	err = wait.PollUntilContextCancel(ctx, pollInterval, true, func(ctx context.Context) (bool, error) {
		_, err := pods.Get(ctx, name, metav1.GetOptions{})
		return apierrors.IsNotFound(err), nil
	})
	if err != nil {
		t.Errorf("wait for pod %s to be deleted: %v", name, err)
	}
}

// dumpPod logs the pod's status and the tail of every container's logs.
func (c *cluster) dumpPod(ctx context.Context, t *testing.T, name string) {
	pods := c.client.CoreV1().Pods(c.namespace)
	pod, err := pods.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Logf("get pod %s for debugging: %v", name, err)
		return
	}
	t.Logf("pod %s phase=%s", name, pod.Status.Phase)
	for _, cs := range pod.Status.ContainerStatuses {
		t.Logf("container %s ready=%t restarts=%d state=%+v", cs.Name, cs.Ready, cs.RestartCount, cs.State)
	}
	tail := int64(logTailLines)
	for _, container := range pod.Spec.Containers {
		logs, err := pods.GetLogs(name, &corev1.PodLogOptions{Container: container.Name, TailLines: &tail}).DoRaw(ctx)
		if err != nil {
			t.Logf("logs of %s: %v", container.Name, err)
			continue
		}
		t.Logf("logs of %s:\n%s", container.Name, logs)
	}
}

// endpointPorts groups the three container ports the e2e test reaches.
type endpointPorts struct {
	inference     int32
	engineMetrics int32
	exporter      int32
}

// endpoints addresses one pod's container ports through the API server's
// port-forward, so no local ports are opened.
type endpoints struct {
	client *http.Client
	host   string // <namespace>.<pod>, resolved by newPodTransport
	ports  endpointPorts
}

// mockerPorts are the ports used by the CPU suite's Dynamo mocker pod.
var mockerPorts = endpointPorts{
	inference:     8000,
	engineMetrics: 8081,
	exporter:      9464,
}

// endpoints returns the HTTP endpoints of the named pod. Idle port-forward
// connections are closed when the test ends, before the pod is reused.
func (c *cluster) endpoints(t *testing.T, pod string) endpoints {
	return c.endpointsWithPorts(t, pod, mockerPorts)
}

// endpointsWithPorts is like endpoints but lets the caller override the ports.
func (c *cluster) endpointsWithPorts(t *testing.T, pod string, ports endpointPorts) endpoints {
	t.Helper()
	transport := newPodTransport(c.client, c.config)
	t.Cleanup(transport.CloseIdleConnections)
	return endpoints{
		client: &http.Client{Transport: transport, Timeout: requestTimeout},
		host:   c.namespace + "." + pod,
		ports:  ports,
	}
}

func (e endpoints) url(port int32, path string) string {
	return fmt.Sprintf("http://%s:%d%s", e.host, port, path)
}

func (e endpoints) inferenceURL(path string) string { return e.url(e.ports.inference, path) }
func (e endpoints) engineMetricsURL() string        { return e.url(e.ports.engineMetrics, metricsPath) }
func (e endpoints) exporterMetricsURL() string      { return e.url(e.ports.exporter, metricsPath) }

// podScraper scrapes a Prometheus text endpoint through the pod transport;
// translator.HTTPScraper always uses http.DefaultClient, which cannot reach
// <namespace>.<pod> addresses.
type podScraper struct {
	client *http.Client
	url    string
}

var _ translator.Scraper = podScraper{}

func (s podScraper) Scrape(ctx context.Context) (map[string]*dto.MetricFamily, error) {
	body, err := get(ctx, s.client, s.url)
	if err != nil {
		return nil, fmt.Errorf("scrape %s: %w", s.url, err)
	}
	// UTF-8 validation accepts engine names with colons, e.g. "vllm:num_requests_running".
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.url, err)
	}
	return families, nil
}

// waitServable polls until the frontend lists the model and both metrics
// endpoints answer. Pod readiness does not cover model discovery.
func waitServable(t *testing.T, ep endpoints, servedModel string) {
	t.Helper()
	err := wait.PollUntilContextTimeout(t.Context(), time.Second, servableTimeout, true, func(ctx context.Context) (bool, error) {
		models, err := get(ctx, ep.client, ep.inferenceURL("/v1/models"))
		if err != nil || !strings.Contains(models, servedModel) {
			return false, nil
		}
		for _, u := range []string{ep.engineMetricsURL(), ep.exporterMetricsURL()} {
			if _, err := get(ctx, ep.client, u); err != nil {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("model %s never became servable: %v", servedModel, err)
	}
}

func get(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", errors.New(resp.Status)
	}
	return string(body), nil
}
