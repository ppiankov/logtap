package sidecar

import (
	"strings"
	"testing"
)

func TestFluentBitConfig(t *testing.T) {
	config := FluentBitConfig("receiver:3100", "lt-abc123", "default")

	if !strings.Contains(config, "[SERVICE]") {
		t.Error("missing [SERVICE] section")
	}
	if !strings.Contains(config, "[INPUT]") {
		t.Error("missing [INPUT] section")
	}
	if !strings.Contains(config, "[OUTPUT]") {
		t.Error("missing [OUTPUT] section")
	}
	if !strings.Contains(config, "default_*") {
		t.Error("missing namespace in path pattern")
	}
	if !strings.Contains(config, "lt-abc123") {
		t.Error("missing session ID")
	}
	if !strings.Contains(config, "receiver:3100") {
		t.Error("missing target host")
	}
}

func TestConfigMapName(t *testing.T) {
	name := ConfigMapName("lt-abc123")
	if name != "logtap-fb-lt-abc123" {
		t.Errorf("got %q, want %q", name, "logtap-fb-lt-abc123")
	}
}

func TestBuildFluentBitContainer(t *testing.T) {
	cfg := SidecarConfig{
		SessionID:  "lt-test",
		Target:     "receiver:3100",
		Image:      "fluent/fluent-bit:3.0",
		MemRequest: "32Mi",
		MemLimit:   "64Mi",
		CPURequest: "50m",
		CPULimit:   "100m",
	}

	c := BuildFluentBitContainer(cfg)

	if c.Name != "logtap-forwarder-lt-test" {
		t.Errorf("container name = %q, want %q", c.Name, "logtap-forwarder-lt-test")
	}
	if c.Image != "fluent/fluent-bit:3.0" {
		t.Errorf("image = %q, want %q", c.Image, "fluent/fluent-bit:3.0")
	}
	if len(c.VolumeMounts) != 2 {
		t.Errorf("volume mounts = %d, want 2", len(c.VolumeMounts))
	}

	// Check config volume mount
	foundConfig := false
	foundLogs := false
	for _, vm := range c.VolumeMounts {
		if vm.MountPath == "/fluent-bit/etc" {
			foundConfig = true
			if !vm.ReadOnly {
				t.Error("config mount should be read-only")
			}
		}
		if vm.MountPath == "/var/log/pods" {
			foundLogs = true
			if !vm.ReadOnly {
				t.Error("logs mount should be read-only")
			}
		}
	}
	if !foundConfig {
		t.Error("missing config volume mount")
	}
	if !foundLogs {
		t.Error("missing logs volume mount")
	}
}

func TestFluentBitVolumes(t *testing.T) {
	volumes := FluentBitVolumes("lt-test")
	if len(volumes) != 2 {
		t.Fatalf("got %d volumes, want 2", len(volumes))
	}

	// Config volume should reference ConfigMap
	if volumes[0].ConfigMap == nil {
		t.Error("first volume should be a ConfigMap volume")
	} else if volumes[0].ConfigMap.Name != "logtap-fb-lt-test" {
		t.Errorf("configmap name = %q, want %q", volumes[0].ConfigMap.Name, "logtap-fb-lt-test")
	}

	// Logs volume should be hostPath
	if volumes[1].HostPath == nil {
		t.Error("second volume should be a HostPath volume")
	} else if volumes[1].HostPath.Path != "/var/log/pods" {
		t.Errorf("hostpath = %q, want %q", volumes[1].HostPath.Path, "/var/log/pods")
	}
}

func TestFluentBitVolumeNames(t *testing.T) {
	names := FluentBitVolumeNames()
	if len(names) != 2 {
		t.Fatalf("got %d names, want 2", len(names))
	}
}
