package k8s

import (
	"testing"
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
