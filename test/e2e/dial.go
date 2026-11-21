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

// Adapted from k8s.io/kubernetes/test/e2e/framework/pod/dial.go: an HTTP
// transport that reaches container ports through the API server's
// port-forward subresource, one stream per connection, so the test needs no
// local listeners.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	"k8s.io/streaming/pkg/httpstream"
)

// newPodTransport returns a transport that port-forwards every connection.
// URLs must use <namespace>.<pod>:<port> as the host, e.g.
// http://default.mss-e2e:8000/v1/models.
func newPodTransport(client kubernetes.Interface, config *rest.Config) *http.Transport {
	// Copy the config once: dialing fills in serializer fields, and connections
	// are dialed concurrently.
	config = rest.CopyConfig(config)
	if config.GroupVersion == nil {
		config.GroupVersion = &schema.GroupVersion{}
	}
	if config.NegotiatedSerializer == nil {
		config.NegotiatedSerializer = scheme.Codecs
	}
	d := &podDialer{client: client, config: config}
	return &http.Transport{
		DialContext: func(_ context.Context, _, addr string) (net.Conn, error) {
			a, err := parsePodAddr(addr)
			if err != nil {
				return nil, err
			}
			return d.dial(a)
		},
	}
}

type podDialer struct {
	client kubernetes.Interface
	config *rest.Config
}

// dial opens a port-forward stream to one container port, preferring
// WebSockets and falling back to SPDY (KEP-4006).
func (d *podDialer) dial(addr podAddr) (conn net.Conn, finalErr error) {
	url := d.client.CoreV1().RESTClient().Post().
		Resource("pods").Namespace(addr.namespace).Name(addr.pod).SubResource("portforward").URL()

	transport, upgrader, err := spdy.RoundTripperFor(d.config)
	if err != nil {
		return nil, fmt.Errorf("create round tripper: %w", err)
	}
	spdyDialer := spdy.NewDialerForStreaming(upgrader, &http.Client{Transport: transport}, http.MethodPost, url)
	wsDialer, err := portforward.NewSPDYOverWebsocketDialerForStreaming(url, d.config)
	if err != nil {
		return nil, fmt.Errorf("create websocket dialer: %w", err)
	}
	dialer := portforward.NewFallbackDialerForStreaming(wsDialer, spdyDialer, func(err error) bool {
		return httpstream.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err)
	})

	streamConn, _, err := dialer.Dial(portforward.PortForwardProtocolV1Name)
	if err != nil {
		return nil, fmt.Errorf("port-forward to %s: %w", addr, err)
	}
	defer func() {
		if finalErr != nil {
			_ = streamConn.Close()
		}
	}()

	headers := http.Header{}
	headers.Set(corev1.StreamType, corev1.StreamTypeError)
	headers.Set(corev1.PortHeader, strconv.Itoa(addr.port))
	headers.Set(corev1.PortForwardRequestIDHeader, "1")

	// The error stream is read-only; the kubelet reports failures to reach the
	// container port on it, asynchronously.
	errorStream, err := streamConn.CreateStream(headers)
	if err != nil {
		return nil, fmt.Errorf("create error stream to %s: %w", addr, err)
	}
	_ = errorStream.Close()
	go func() {
		// Not t.Logf: the stream can outlive the test that dialed it.
		message, err := io.ReadAll(errorStream)
		switch {
		case err != nil:
			log.Printf("port-forward to %s: read error stream: %v", addr, err)
		case len(message) > 0:
			log.Printf("port-forward to %s: %v", addr, errors.New(string(message)))
		}
	}()

	headers.Set(corev1.StreamType, corev1.StreamTypeData)
	dataStream, err := streamConn.CreateStream(headers)
	if err != nil {
		return nil, fmt.Errorf("create data stream to %s: %w", addr, err)
	}
	return &podStream{Stream: dataStream, conn: streamConn, addr: addr}, nil
}

// podAddr is a container port in a pod, written <namespace>.<pod>:<port>.
type podAddr struct {
	namespace, pod string
	port           int
}

var _ net.Addr = podAddr{}

func (a podAddr) Network() string { return "port-forwarding" }
func (a podAddr) String() string  { return fmt.Sprintf("%s.%s:%d", a.namespace, a.pod, a.port) }

var podAddrRegex = regexp.MustCompile(`^([^.]+)\.([^:]+):(\d+)$`)

func parsePodAddr(addr string) (podAddr, error) {
	parts := podAddrRegex.FindStringSubmatch(addr)
	if parts == nil {
		return podAddr{}, fmt.Errorf("%q: must match <namespace>.<pod>:<port>", addr)
	}
	port, err := strconv.Atoi(parts[3])
	if err != nil {
		return podAddr{}, fmt.Errorf("%q: invalid port: %w", addr, err)
	}
	return podAddr{namespace: parts[1], pod: parts[2], port: port}, nil
}

// podStream adapts a port-forward data stream to net.Conn. Deadlines are
// not supported; callers bound requests with an http.Client timeout or context.
type podStream struct {
	httpstream.Stream
	conn httpstream.Connection
	addr podAddr
}

var _ net.Conn = &podStream{}

func (s *podStream) Close() error {
	_ = s.Stream.Close()
	return s.conn.Close()
}

func (s *podStream) LocalAddr() net.Addr              { return apiServerAddr{} }
func (s *podStream) RemoteAddr() net.Addr             { return s.addr }
func (s *podStream) SetDeadline(time.Time) error      { return nil }
func (s *podStream) SetReadDeadline(time.Time) error  { return nil }
func (s *podStream) SetWriteDeadline(time.Time) error { return nil }

// apiServerAddr is the local end of a port-forward stream: the API server.
type apiServerAddr struct{}

func (apiServerAddr) Network() string { return "port-forwarding" }
func (apiServerAddr) String() string  { return "apiserver" }
