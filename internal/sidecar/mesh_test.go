package sidecar

import "testing"

func TestMeshBypass_Linkerd(t *testing.T) {
	existing := map[string]string{
		"linkerd.io/inject": "enabled",
	}
	result := MeshBypassAnnotations(existing, "3100")
	if len(result) != 1 {
		t.Fatalf("got %d annotations, want 1", len(result))
	}
	if result["config.linkerd.io/skip-outbound-ports"] != "3100" {
		t.Errorf("skip-outbound-ports = %q, want %q", result["config.linkerd.io/skip-outbound-ports"], "3100")
	}
}

func TestMeshBypass_Istio(t *testing.T) {
	existing := map[string]string{
		"sidecar.istio.io/inject": "true",
	}
	result := MeshBypassAnnotations(existing, "3100")
	if len(result) != 1 {
		t.Fatalf("got %d annotations, want 1", len(result))
	}
	if result["traffic.sidecar.istio.io/excludeOutboundPorts"] != "3100" {
		t.Errorf("excludeOutboundPorts = %q, want %q", result["traffic.sidecar.istio.io/excludeOutboundPorts"], "3100")
	}
}

func TestMeshBypass_IstioRev(t *testing.T) {
	existing := map[string]string{
		"istio.io/rev": "canary",
	}
	result := MeshBypassAnnotations(existing, "9090")
	if len(result) != 1 {
		t.Fatalf("got %d annotations, want 1", len(result))
	}
	if result["traffic.sidecar.istio.io/excludeOutboundPorts"] != "9090" {
		t.Errorf("excludeOutboundPorts = %q, want %q", result["traffic.sidecar.istio.io/excludeOutboundPorts"], "9090")
	}
}

func TestMeshBypass_NoMesh(t *testing.T) {
	existing := map[string]string{
		"app": "my-app",
	}
	result := MeshBypassAnnotations(existing, "3100")
	if len(result) != 0 {
		t.Errorf("got %d annotations, want 0", len(result))
	}
}

func TestMeshBypass_ExistingPorts(t *testing.T) {
	existing := map[string]string{
		"linkerd.io/inject":                             "enabled",
		"config.linkerd.io/skip-outbound-ports":         "8080,9090",
		"sidecar.istio.io/inject":                       "true",
		"traffic.sidecar.istio.io/excludeOutboundPorts": "8080",
	}
	result := MeshBypassAnnotations(existing, "3100")
	if result["config.linkerd.io/skip-outbound-ports"] != "8080,9090,3100" {
		t.Errorf("linkerd ports = %q, want %q", result["config.linkerd.io/skip-outbound-ports"], "8080,9090,3100")
	}
	if result["traffic.sidecar.istio.io/excludeOutboundPorts"] != "8080,3100" {
		t.Errorf("istio ports = %q, want %q", result["traffic.sidecar.istio.io/excludeOutboundPorts"], "8080,3100")
	}
}

func TestMeshBypass_ExistingPortDuplicate(t *testing.T) {
	existing := map[string]string{
		"linkerd.io/inject":                     "enabled",
		"config.linkerd.io/skip-outbound-ports": "3100,8080",
	}
	result := MeshBypassAnnotations(existing, "3100")
	if result["config.linkerd.io/skip-outbound-ports"] != "3100,8080" {
		t.Errorf("ports = %q, want %q (no duplicate)", result["config.linkerd.io/skip-outbound-ports"], "3100,8080")
	}
}

func TestMeshBypass_EmptyPort(t *testing.T) {
	existing := map[string]string{
		"linkerd.io/inject": "enabled",
	}
	result := MeshBypassAnnotations(existing, "")
	if result != nil {
		t.Errorf("got %v, want nil for empty port", result)
	}
}

func TestMeshBypass_BothMeshes(t *testing.T) {
	existing := map[string]string{
		"linkerd.io/inject":       "enabled",
		"sidecar.istio.io/inject": "true",
	}
	result := MeshBypassAnnotations(existing, "3100")
	if len(result) != 2 {
		t.Fatalf("got %d annotations, want 2", len(result))
	}
}

func TestExtractPort(t *testing.T) {
	tests := []struct {
		target string
		want   string
	}{
		{"192.168.1.1:3100", "3100"},
		{"logtap-recv:9000", "9000"},
		{"logtap-recv.ns.svc.cluster.local:3100", "3100"},
		{":3100", "3100"},
		{"badformat", ""},
	}
	for _, tt := range tests {
		got := extractPort(tt.target)
		if got != tt.want {
			t.Errorf("extractPort(%q) = %q, want %q", tt.target, got, tt.want)
		}
	}
}
