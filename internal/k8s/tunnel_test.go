package k8s

import (
	"strings"
	"testing"

	"k8s.io/client-go/rest"
)

func TestPortForwardSpec(t *testing.T) {
	spec := PortForwardSpec{
		Namespace:  "logtap",
		PodName:    "logtap-receiver",
		RemotePort: 9000,
		LocalPort:  0,
	}

	if spec.Namespace != "logtap" {
		t.Errorf("Namespace = %q, want %q", spec.Namespace, "logtap")
	}
	if spec.PodName != "logtap-receiver" {
		t.Errorf("PodName = %q, want %q", spec.PodName, "logtap-receiver")
	}
	if spec.RemotePort != 9000 {
		t.Errorf("RemotePort = %d, want 9000", spec.RemotePort)
	}
	if spec.LocalPort != 0 {
		t.Errorf("LocalPort = %d, want 0 (dynamic)", spec.LocalPort)
	}
}

func TestNewPortForwardTunnel_NilConfig(t *testing.T) {
	_, err := NewPortForwardTunnel(nil, nil, PortForwardSpec{}, nil, nil)
	if err == nil {
		t.Fatal("expected error for nil rest config")
	}
}

func TestNewPortForwardTunnel_BadTLS(t *testing.T) {
	cfg := &rest.Config{
		Host: "https://localhost:6443",
		TLSClientConfig: rest.TLSClientConfig{
			CertData: []byte("not-a-cert"),
			KeyData:  []byte("not-a-key"),
		},
	}
	_, err := NewPortForwardTunnel(cfg, nil, PortForwardSpec{
		Namespace:  "test",
		PodName:    "test-pod",
		RemotePort: 3100,
	}, nil, nil)
	if err == nil {
		t.Fatal("expected error for bad TLS config")
	}
	if !strings.Contains(err.Error(), "SPDY") {
		t.Errorf("error = %q, want to contain 'SPDY'", err)
	}
}

func TestNewPortForwardTunnel_InvalidHostURL(t *testing.T) {
	cfg := &rest.Config{
		Host: "://",
	}
	_, err := NewPortForwardTunnel(cfg, nil, PortForwardSpec{
		Namespace:  "test",
		PodName:    "test-pod",
		RemotePort: 3100,
	}, nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid host URL")
	}
}

func TestPortForwardTunnel_StopAndReady(t *testing.T) {
	// Verify Stop and ReadyCh don't panic on a manually constructed tunnel.
	stopCh := make(chan struct{})
	readyCh := make(chan struct{})
	tunnel := &PortForwardTunnel{
		spec: PortForwardSpec{
			Namespace:  "test",
			PodName:    "test-pod",
			RemotePort: 3100,
		},
		stopCh:  stopCh,
		readyCh: readyCh,
	}

	ch := tunnel.ReadyCh()
	if ch == nil {
		t.Fatal("ReadyCh returned nil")
	}

	tunnel.Stop()

	// Verify stopCh is closed
	select {
	case <-stopCh:
	default:
		t.Fatal("stopCh should be closed after Stop()")
	}
}
