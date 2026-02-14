package k8s

import (
	"fmt"
	"io"
	"net/http"
	"net/url"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// PortForwardSpec describes a port-forward tunnel to a pod.
type PortForwardSpec struct {
	Namespace  string
	PodName    string
	RemotePort int
	LocalPort  int // 0 = allocate dynamically
}

// PortForwardTunnel manages a port-forward connection to a pod.
type PortForwardTunnel struct {
	spec    PortForwardSpec
	fw      *portforward.PortForwarder
	stopCh  chan struct{}
	readyCh chan struct{}
}

// NewPortForwardTunnel creates a port-forward tunnel.
func NewPortForwardTunnel(restConfig *rest.Config, cs kubernetes.Interface, spec PortForwardSpec, out, errOut io.Writer) (*PortForwardTunnel, error) {
	if restConfig == nil {
		return nil, fmt.Errorf("REST config required for port-forward")
	}

	transport, upgrader, err := spdy.RoundTripperFor(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create SPDY transport: %w", err)
	}

	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", spec.Namespace, spec.PodName)
	hostURL, err := url.Parse(restConfig.Host)
	if err != nil {
		return nil, fmt.Errorf("parse host URL: %w", err)
	}
	hostURL.Path = path

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, hostURL)

	ports := []string{fmt.Sprintf("%d:%d", spec.LocalPort, spec.RemotePort)}
	stopCh := make(chan struct{})
	readyCh := make(chan struct{})

	fw, err := portforward.New(dialer, ports, stopCh, readyCh, out, errOut)
	if err != nil {
		return nil, fmt.Errorf("create port-forwarder: %w", err)
	}

	return &PortForwardTunnel{
		spec:    spec,
		fw:      fw,
		stopCh:  stopCh,
		readyCh: readyCh,
	}, nil
}

// Run starts the port-forward. Blocks until stopped or error.
func (t *PortForwardTunnel) Run() error {
	return t.fw.ForwardPorts()
}

// Stop closes the port-forward tunnel.
func (t *PortForwardTunnel) Stop() {
	close(t.stopCh)
}

// ReadyCh returns a channel that is closed when the tunnel is ready.
func (t *PortForwardTunnel) ReadyCh() <-chan struct{} {
	return t.readyCh
}

// GetLocalPort returns the actual local port after the tunnel is ready.
func (t *PortForwardTunnel) GetLocalPort() (int, error) {
	ports, err := t.fw.GetPorts()
	if err != nil {
		return 0, err
	}
	if len(ports) == 0 {
		return 0, fmt.Errorf("no forwarded ports")
	}
	return int(ports[0].Local), nil
}
