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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	"sigs.k8s.io/yaml"
)

const (
	// The manifest is the single source for images, ports and the model.
	manifestPath = "manifests/dynamo-mocker.yaml"
	namespace    = "default"

	// Named container ports in the manifest.
	inferencePortName     = "inference"
	engineMetricsPortName = "engine-metrics"
	exporterPortName      = "contract"

	// The first engine downloads the tokenizer, so readiness can take a while.
	podReadyTimeout  = 10 * time.Minute
	servableTimeout  = 2 * time.Minute
	podDeleteTimeout = 2 * time.Minute
	pollInterval     = 2 * time.Second
	logTailLines     = 50
)

// mockerEngineTypes maps a catalog profile name to the mocker's --engine-type.
var mockerEngineTypes = map[string]string{
	"vllm":    "vllm",
	"sglang":  "sglang",
	"trt-llm": "trtllm",
}

// cluster wraps the clients for the kind cluster created by hack/test-e2e.sh.
type cluster struct {
	config *rest.Config
	client kubernetes.Interface
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
	return &cluster{config: config, client: client}
}

// renderPod fills the manifest template in for one engine.
func renderPod(t *testing.T, engine string) *corev1.Pod {
	t.Helper()
	engineType, ok := mockerEngineTypes[engine]
	if !ok {
		t.Fatalf("no mocker engine type for profile %q", engine)
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	rendered := strings.NewReplacer("__ENGINE_TYPE__", engineType, "__PROFILE__", engine).Replace(string(raw))
	pod := &corev1.Pod{}
	if err := yaml.Unmarshal([]byte(rendered), pod); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	return pod
}

// deploy creates the pod, waits until it is ready, and registers a cleanup
// that dumps its state on failure and deletes it, so the next engine can
// reuse the pod name.
func (c *cluster) deploy(t *testing.T, pod *corev1.Pod) {
	t.Helper()
	pods := c.client.CoreV1().Pods(namespace)
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
	pods := c.client.CoreV1().Pods(namespace)
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
	pods := c.client.CoreV1().Pods(namespace)
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

// forward port-forwards the given pod ports to random local ports on
// 127.0.0.1, and returns the local port for each pod port.
func (c *cluster) forward(t *testing.T, podName string, podPorts ...int32) map[int32]int {
	t.Helper()
	transport, upgrader, err := spdy.RoundTripperFor(c.config)
	if err != nil {
		t.Fatalf("create port-forward transport: %v", err)
	}
	url := c.client.CoreV1().RESTClient().Post().
		Resource("pods").Namespace(namespace).Name(podName).SubResource("portforward").URL()
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, url)

	specs := make([]string, 0, len(podPorts))
	for _, p := range podPorts {
		specs = append(specs, fmt.Sprintf("0:%d", p))
	}
	stop, ready := make(chan struct{}), make(chan struct{})
	// Not `localhost`: it may resolve to ::1, where another process can hold the port.
	forwarder, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"}, specs, stop, ready, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("create port-forward: %v", err)
	}
	errs := make(chan error, 1)
	go func() { errs <- forwarder.ForwardPorts() }()
	t.Cleanup(func() { close(stop) })

	select {
	case <-ready:
	case err := <-errs:
		t.Fatalf("port-forward to %s: %v", podName, err)
	case <-time.After(servableTimeout):
		t.Fatalf("port-forward to %s: not ready after %s", podName, servableTimeout)
	}
	forwarded, err := forwarder.GetPorts()
	if err != nil {
		t.Fatalf("read forwarded ports: %v", err)
	}
	local := make(map[int32]int, len(forwarded))
	for _, p := range forwarded {
		local[int32(p.Remote)] = int(p.Local)
	}
	return local
}

// containerPort returns the pod port with the given name.
func containerPort(t *testing.T, pod *corev1.Pod, name string) int32 {
	t.Helper()
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			if port.Name == name {
				return port.ContainerPort
			}
		}
	}
	t.Fatalf("manifest has no container port named %q", name)
	return 0
}

// servedModel returns the model the mocker serves, from its --model-path flag.
func servedModel(t *testing.T, pod *corev1.Pod) string {
	t.Helper()
	const flag = "--model-path="
	for _, container := range pod.Spec.Containers {
		for _, arg := range append(container.Command, container.Args...) {
			if model, ok := strings.CutPrefix(arg, flag); ok {
				return model
			}
		}
	}
	t.Fatalf("manifest has no %s argument", flag)
	return ""
}

// waitServable polls until the frontend lists the model and both metrics
// endpoints answer. Pod readiness does not cover model discovery.
func waitServable(t *testing.T, ep endpoints, model string) {
	t.Helper()
	err := wait.PollUntilContextTimeout(t.Context(), time.Second, servableTimeout, true, func(ctx context.Context) (bool, error) {
		models, err := get(ctx, ep.inferenceURL("/v1/models"))
		if err != nil || !strings.Contains(models, model) {
			return false, nil
		}
		for _, u := range []string{ep.engineMetricsURL(), ep.exporterMetricsURL()} {
			if _, err := get(ctx, u); err != nil {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("model %s never became servable: %v", model, err)
	}
}

func get(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
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
